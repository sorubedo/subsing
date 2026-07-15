package main

import (
	"context"
	stdjson "encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/sagernet/sing-box/option"
	SJSON "github.com/sagernet/sing/common/json"
	"github.com/sagernet/sing/common/json/badjson"
	"subsing/internal/providerparser"
)

const (
	defaultUserAgent = "subsing/1.0"
	maxResponseSize  = 64 << 20
)

type Result struct {
	Files   int
	Skipped bool
}

type remoteProvider struct {
	Type      string `json:"type"`
	Tag       string `json:"tag"`
	URL       string `json:"url"`
	Exclude   string `json:"exclude"`
	Include   string `json:"include"`
	UserAgent string `json:"user_agent"`
}

type groupExtension struct {
	Providers      []string `json:"_providers"`
	Exclude        string   `json:"_exclude"`
	Include        string   `json:"_include"`
	UseAllProvider bool     `json:"_use_all_providers"`
}

type providerResult struct {
	tag          string
	outbounds    []option.Outbound
	endpoints    []option.Endpoint
	rawOutbounds []*badjson.JSONObject
	rawEndpoints []*badjson.JSONObject
	tags         []string
}

type Converter struct {
	client *http.Client
}

func NewConverter() *Converter {
	return &Converter{client: &http.Client{Timeout: 30 * time.Second}}
}

func Run(ctx context.Context, inputDir, outputDir string) (Result, error) {
	input, output, err := validateDirectories(inputDir, outputDir)
	if err != nil {
		return Result{}, err
	}
	nonEmpty, err := directoryNonEmpty(output)
	if err != nil {
		return Result{}, err
	}
	if nonEmpty {
		return Result{Skipped: true}, nil
	}

	entries, err := os.ReadDir(input)
	if err != nil {
		return Result{}, fmt.Errorf("read input directory: %w", err)
	}
	var names []string
	for _, entry := range entries {
		if entry.Type().IsRegular() {
			ext := strings.ToLower(filepath.Ext(entry.Name()))
			if ext == ".json" || ext == ".jsonc" {
				names = append(names, entry.Name())
			}
		}
	}
	sort.Strings(names)
	if len(names) == 0 {
		return Result{}, errors.New("input directory contains no .json or .jsonc files")
	}

	converter := NewConverter()
	converted := make(map[string][]byte, len(names))
	for _, name := range names {
		content, readErr := os.ReadFile(filepath.Join(input, name))
		if readErr != nil {
			return Result{}, fmt.Errorf("%s: read: %w", name, readErr)
		}
		outputContent, convertErr := converter.Convert(ctx, content)
		if convertErr != nil {
			return Result{}, fmt.Errorf("%s: %w", name, convertErr)
		}
		converted[name] = outputContent
	}
	skipped, err := publish(output, names, converted)
	if err != nil {
		return Result{}, err
	}
	if skipped {
		return Result{Skipped: true}, nil
	}
	return Result{Files: len(names)}, nil
}

func validateDirectories(inputDir, outputDir string) (string, string, error) {
	input, err := filepath.Abs(inputDir)
	if err != nil {
		return "", "", fmt.Errorf("resolve input directory: %w", err)
	}
	input, err = filepath.EvalSymlinks(input)
	if err != nil {
		return "", "", fmt.Errorf("resolve input directory: %w", err)
	}
	info, err := os.Stat(input)
	if err != nil {
		return "", "", fmt.Errorf("stat input directory: %w", err)
	}
	if !info.IsDir() {
		return "", "", errors.New("input path is not a directory")
	}
	output, err := filepath.Abs(outputDir)
	if err != nil {
		return "", "", fmt.Errorf("resolve output directory: %w", err)
	}
	output, err = resolvePotentialPath(output)
	if err != nil {
		return "", "", fmt.Errorf("resolve output directory: %w", err)
	}
	if pathsOverlap(input, output) {
		return "", "", errors.New("input and output directories must not overlap")
	}
	return input, output, nil
}

func resolvePotentialPath(path string) (string, error) {
	current := filepath.Clean(path)
	var suffix []string
	for {
		_, err := os.Lstat(current)
		if err == nil {
			resolved, resolveErr := filepath.EvalSymlinks(current)
			if resolveErr != nil {
				return "", resolveErr
			}
			for index := len(suffix) - 1; index >= 0; index-- {
				resolved = filepath.Join(resolved, suffix[index])
			}
			return resolved, nil
		}
		if !os.IsNotExist(err) {
			return "", err
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", err
		}
		suffix = append(suffix, filepath.Base(current))
		current = parent
	}
}

func pathsOverlap(a, b string) bool {
	relAB, errAB := filepath.Rel(a, b)
	relBA, errBA := filepath.Rel(b, a)
	inside := func(rel string, err error) bool {
		return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
	}
	return inside(relAB, errAB) || inside(relBA, errBA)
}

func directoryNonEmpty(path string) (bool, error) {
	dir, err := os.Open(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("open output directory: %w", err)
	}
	defer dir.Close()
	info, err := dir.Stat()
	if err != nil {
		return false, fmt.Errorf("stat output directory: %w", err)
	}
	if !info.IsDir() {
		return false, errors.New("output path is not a directory")
	}
	_, err = dir.Readdirnames(1)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, io.EOF) {
		return false, nil
	}
	return false, fmt.Errorf("read output directory: %w", err)
}

func publish(output string, names []string, files map[string][]byte) (bool, error) {
	parent := filepath.Dir(output)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return false, fmt.Errorf("create output parent: %w", err)
	}
	stage, err := os.MkdirTemp(parent, ".subsing-stage-")
	if err != nil {
		return false, fmt.Errorf("create staging directory: %w", err)
	}
	defer os.RemoveAll(stage)
	for _, name := range names {
		if err = os.WriteFile(filepath.Join(stage, name), files[name], 0o644); err != nil {
			return false, fmt.Errorf("stage %s: %w", name, err)
		}
	}
	if info, statErr := os.Stat(output); statErr == nil {
		if !info.IsDir() {
			return false, errors.New("output path is not a directory")
		}
		entries, readErr := os.ReadDir(output)
		if readErr != nil {
			return false, fmt.Errorf("read output directory: %w", readErr)
		}
		if len(entries) != 0 {
			return true, nil
		}
		if err = os.Remove(output); err != nil {
			// A bind-mounted output directory cannot be removed. Write the
			// staged files into it instead, so container mounts work as expected.
			for _, name := range names {
				content, readErr := os.ReadFile(filepath.Join(stage, name))
				if readErr != nil {
					return false, fmt.Errorf("read staged %s: %w", name, readErr)
				}
				if writeErr := os.WriteFile(filepath.Join(output, name), content, 0o644); writeErr != nil {
					return false, fmt.Errorf("publish %s: %w", name, writeErr)
				}
			}
			return false, nil
		}
	} else if !os.IsNotExist(statErr) {
		return false, fmt.Errorf("stat output directory: %w", statErr)
	}
	if err = os.Rename(stage, output); err != nil {
		return false, fmt.Errorf("publish output directory: %w", err)
	}
	return false, nil
}

func (c *Converter) Convert(ctx context.Context, input []byte) ([]byte, error) {
	parseCtx := ctx
	var root badjson.JSONObject
	if err := SJSON.UnmarshalContext(parseCtx, input, &root); err != nil {
		return nil, fmt.Errorf("parse pseudo config: %w", err)
	}
	providers, err := decodeProviders(parseCtx, &root)
	if err != nil {
		return nil, err
	}
	results := make([]providerResult, 0, len(providers))
	for _, provider := range providers {
		result, fetchErr := c.loadProvider(parseCtx, provider)
		if fetchErr != nil {
			return nil, fmt.Errorf("provider %q: %w", provider.Tag, fetchErr)
		}
		results = append(results, result)
	}
	root.Remove("_providers")
	if err = expandConfig(parseCtx, &root, results); err != nil {
		return nil, err
	}
	compact, err := root.MarshalJSONContext(parseCtx)
	if err != nil {
		return nil, fmt.Errorf("encode output: %w", err)
	}
	var pretty strings.Builder
	encoder := stdjson.NewEncoder(&pretty)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	var generic any
	if err = stdjson.Unmarshal(compact, &generic); err != nil {
		return nil, fmt.Errorf("prepare formatted output: %w", err)
	}
	if err = encoder.Encode(generic); err != nil {
		return nil, fmt.Errorf("format output: %w", err)
	}
	return []byte(pretty.String()), nil
}

func decodeProviders(ctx context.Context, root *badjson.JSONObject) ([]remoteProvider, error) {
	value, found := root.Get("_providers")
	if !found {
		return nil, nil
	}
	array, ok := value.(badjson.JSONArray)
	if !ok {
		return nil, errors.New("_providers must be an array")
	}
	providers := make([]remoteProvider, 0, len(array))
	seen := make(map[string]bool)
	for index, item := range array {
		object, ok := item.(*badjson.JSONObject)
		if !ok {
			return nil, fmt.Errorf("_providers[%d] must be an object", index)
		}
		content, err := object.MarshalJSONContext(ctx)
		if err != nil {
			return nil, fmt.Errorf("encode _providers[%d]: %w", index, err)
		}
		var provider remoteProvider
		if err = SJSON.Unmarshal(content, &provider); err != nil {
			return nil, fmt.Errorf("decode _providers[%d]: %w", index, err)
		}
		if provider.Type != "remote" {
			return nil, fmt.Errorf("_providers[%d]: type must be remote", index)
		}
		if provider.Tag == "" {
			return nil, fmt.Errorf("_providers[%d]: tag is required", index)
		}
		if seen[provider.Tag] {
			return nil, fmt.Errorf("duplicate provider tag %q", provider.Tag)
		}
		seen[provider.Tag] = true
		parsedURL, err := url.Parse(provider.URL)
		if err != nil || parsedURL.Host == "" || (parsedURL.Scheme != "http" && parsedURL.Scheme != "https") {
			return nil, fmt.Errorf("_providers[%d]: url must be an absolute HTTP(S) URL", index)
		}
		providers = append(providers, provider)
	}
	return providers, nil
}

func (c *Converter) loadProvider(ctx context.Context, provider remoteProvider) (providerResult, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, provider.URL, nil)
	if err != nil {
		return providerResult{}, err
	}
	userAgent := provider.UserAgent
	if userAgent == "" {
		userAgent = defaultUserAgent
	}
	req.Header.Set("User-Agent", userAgent)
	response, err := c.client.Do(req)
	if err != nil {
		return providerResult{}, fmt.Errorf("download: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return providerResult{}, fmt.Errorf("download: unexpected HTTP status %s", response.Status)
	}
	content, err := io.ReadAll(io.LimitReader(response.Body, maxResponseSize+1))
	if err != nil {
		return providerResult{}, fmt.Errorf("read response: %w", err)
	}
	if len(content) > maxResponseSize {
		return providerResult{}, fmt.Errorf("response exceeds %d MiB", maxResponseSize>>20)
	}
	decoded, _ := providerparser.DecodeBase64URLSafe(string(content))
	if rawOutbounds, rawEndpoints, recognized, parseErr := parseSingBoxSubscription(ctx, decoded); recognized {
		if parseErr != nil {
			return providerResult{}, fmt.Errorf("parse sing-box subscription: %w", parseErr)
		}
		return prepareRawProvider(provider, rawOutbounds, rawEndpoints)
	}
	outbounds, endpoints, err := providerparser.ParseSubscription(ctx, decoded, nil, nil, provider.Tag)
	if err != nil {
		return providerResult{}, fmt.Errorf("parse subscription: %w", err)
	}
	exclude, err := compileOptionalRegexp(provider.Exclude, "exclude")
	if err != nil {
		return providerResult{}, err
	}
	includeRegex, err := compileOptionalRegexp(provider.Include, "include")
	if err != nil {
		return providerResult{}, err
	}
	filteredOutbounds := outbounds[:0]
	for _, outbound := range outbounds {
		if matchesFilters(outbound.Tag, exclude, includeRegex) {
			filteredOutbounds = append(filteredOutbounds, outbound)
		}
	}
	filteredEndpoints := endpoints[:0]
	for _, endpoint := range endpoints {
		if matchesFilters(endpoint.Tag, exclude, includeRegex) {
			filteredEndpoints = append(filteredEndpoints, endpoint)
		}
	}
	outbounds = filteredOutbounds
	endpoints = filteredEndpoints

	tags := make([]string, 0, len(outbounds)+len(endpoints))
	seenOutbounds := make(map[string]bool)
	for index := range outbounds {
		outbounds[index].Tag = uniqueProviderTag(provider.Tag, outbounds[index].Tag, index, false, seenOutbounds)
		tags = append(tags, outbounds[index].Tag)
	}
	seenEndpoints := make(map[string]bool)
	for index := range endpoints {
		endpoints[index].Tag = uniqueProviderTag(provider.Tag, endpoints[index].Tag, index, true, seenEndpoints)
		tags = append(tags, endpoints[index].Tag)
	}
	return providerResult{tag: provider.Tag, outbounds: outbounds, endpoints: endpoints, tags: tags}, nil
}

func parseSingBoxSubscription(ctx context.Context, content string) ([]*badjson.JSONObject, []*badjson.JSONObject, bool, error) {
	var root badjson.JSONObject
	if err := SJSON.UnmarshalContext(ctx, []byte(content), &root); err != nil {
		return nil, nil, false, nil
	}
	_, hasOutbounds := root.Get("outbounds")
	_, hasEndpoints := root.Get("endpoints")
	if !hasOutbounds && !hasEndpoints {
		return nil, nil, false, nil
	}
	outbounds, err := objectArray(&root, "outbounds")
	if err != nil {
		return nil, nil, true, err
	}
	endpoints, err := objectArray(&root, "endpoints")
	if err != nil {
		return nil, nil, true, err
	}
	filtered := outbounds[:0]
	for index, outbound := range outbounds {
		typeName, ok := stringField(outbound, "type")
		if !ok || typeName == "" {
			return nil, nil, true, fmt.Errorf("outbounds[%d]: type is required", index)
		}
		switch typeName {
		case "direct", "block", "dns", "selector", "urltest", "pass":
			continue
		default:
			filtered = append(filtered, outbound)
		}
	}
	if len(filtered) == 0 && len(endpoints) == 0 {
		return nil, nil, true, errors.New("no servers found")
	}
	return filtered, endpoints, true, nil
}

func prepareRawProvider(provider remoteProvider, outbounds, endpoints []*badjson.JSONObject) (providerResult, error) {
	exclude, err := compileOptionalRegexp(provider.Exclude, "exclude")
	if err != nil {
		return providerResult{}, err
	}
	includeRegex, err := compileOptionalRegexp(provider.Include, "include")
	if err != nil {
		return providerResult{}, err
	}
	allOriginalTags := make(map[string]bool)
	for _, object := range append(append([]*badjson.JSONObject{}, outbounds...), endpoints...) {
		if tag, ok := stringField(object, "tag"); ok && tag != "" {
			allOriginalTags[tag] = true
		}
	}
	filter := func(objects []*badjson.JSONObject) []*badjson.JSONObject {
		result := objects[:0]
		for _, object := range objects {
			tag, _ := stringField(object, "tag")
			if matchesFilters(tag, exclude, includeRegex) {
				if detour, ok := stringField(object, "detour"); ok && allOriginalTags[detour] {
					object.Put("detour", provider.Tag+"/"+detour)
				}
				result = append(result, object)
			}
		}
		return result
	}
	outbounds = filter(outbounds)
	endpoints = filter(endpoints)
	tags := make([]string, 0, len(outbounds)+len(endpoints))
	seenOutbounds := make(map[string]bool)
	for index, object := range outbounds {
		tag, _ := stringField(object, "tag")
		tag = uniqueProviderTag(provider.Tag, tag, index, false, seenOutbounds)
		object.Put("tag", tag)
		tags = append(tags, tag)
	}
	seenEndpoints := make(map[string]bool)
	for index, object := range endpoints {
		tag, _ := stringField(object, "tag")
		tag = uniqueProviderTag(provider.Tag, tag, index, true, seenEndpoints)
		object.Put("tag", tag)
		tags = append(tags, tag)
	}
	return providerResult{tag: provider.Tag, rawOutbounds: outbounds, rawEndpoints: endpoints, tags: tags}, nil
}

func uniqueProviderTag(provider, node string, index int, endpoint bool, seen map[string]bool) string {
	if node == "" {
		if endpoint {
			node = fmt.Sprintf("endpoint-%d", index)
		} else {
			node = fmt.Sprint(index)
		}
	}
	base := provider + "/" + node
	tag := base
	for suffix := 2; seen[tag]; suffix++ {
		tag = fmt.Sprintf("%s (%d)", base, suffix)
	}
	seen[tag] = true
	return tag
}

func compileOptionalRegexp(expression, field string) (*regexp.Regexp, error) {
	if expression == "" {
		return nil, nil
	}
	compiled, err := regexp.Compile(expression)
	if err != nil {
		return nil, fmt.Errorf("invalid %s regular expression: %w", field, err)
	}
	return compiled, nil
}

func matchesFilters(tag string, exclude, includeRegex *regexp.Regexp) bool {
	return (exclude == nil || !exclude.MatchString(tag)) && (includeRegex == nil || includeRegex.MatchString(tag))
}

func expandConfig(ctx context.Context, root *badjson.JSONObject, providers []providerResult) error {
	providerByTag := make(map[string]providerResult, len(providers))
	for _, provider := range providers {
		providerByTag[provider.tag] = provider
	}
	outboundArray, err := objectArray(root, "outbounds")
	if err != nil {
		return err
	}
	endpointArray, err := objectArray(root, "endpoints")
	if err != nil {
		return err
	}
	staticTags := make(map[string]bool)
	for _, object := range append(append([]*badjson.JSONObject{}, outboundArray...), endpointArray...) {
		if tag, ok := stringField(object, "tag"); ok && tag != "" {
			if staticTags[tag] {
				return fmt.Errorf("duplicate existing outbound or endpoint tag %q", tag)
			}
			staticTags[tag] = true
		}
	}
	for _, provider := range providers {
		for _, tag := range provider.tags {
			if staticTags[tag] {
				return fmt.Errorf("generated tag %q conflicts with existing tag", tag)
			}
			staticTags[tag] = true
		}
		for _, outbound := range provider.outbounds {
			object, marshalErr := outboundObject(ctx, outbound)
			if marshalErr != nil {
				return marshalErr
			}
			outboundArray = append(outboundArray, object)
		}
		for _, endpoint := range provider.endpoints {
			object, marshalErr := endpointObject(ctx, endpoint)
			if marshalErr != nil {
				return marshalErr
			}
			endpointArray = append(endpointArray, object)
		}
		outboundArray = append(outboundArray, provider.rawOutbounds...)
		endpointArray = append(endpointArray, provider.rawEndpoints...)
	}
	for index, outbound := range outboundArray {
		typeName, _ := stringField(outbound, "type")
		if typeName != "selector" && typeName != "urltest" {
			continue
		}
		if err = expandGroup(ctx, outbound, index, providers, providerByTag, staticTags); err != nil {
			return err
		}
	}
	root.Put("outbounds", toJSONArray(outboundArray))
	if len(endpointArray) > 0 {
		root.Put("endpoints", toJSONArray(endpointArray))
	}
	return nil
}

func objectArray(root *badjson.JSONObject, key string) ([]*badjson.JSONObject, error) {
	value, found := root.Get(key)
	if !found {
		return nil, nil
	}
	array, ok := value.(badjson.JSONArray)
	if !ok {
		return nil, fmt.Errorf("%s must be an array", key)
	}
	objects := make([]*badjson.JSONObject, 0, len(array))
	for index, item := range array {
		object, ok := item.(*badjson.JSONObject)
		if !ok {
			return nil, fmt.Errorf("%s[%d] must be an object", key, index)
		}
		objects = append(objects, object)
	}
	return objects, nil
}

func toJSONArray(objects []*badjson.JSONObject) badjson.JSONArray {
	array := make(badjson.JSONArray, len(objects))
	for index, object := range objects {
		array[index] = object
	}
	return array
}

func expandGroup(ctx context.Context, object *badjson.JSONObject, index int, providers []providerResult, providerByTag map[string]providerResult, validTags map[string]bool) error {
	content, err := object.MarshalJSONContext(ctx)
	if err != nil {
		return fmt.Errorf("encode outbounds[%d]: %w", index, err)
	}
	var extension groupExtension
	if err = SJSON.Unmarshal(content, &extension); err != nil {
		return fmt.Errorf("decode group extensions in outbounds[%d]: %w", index, err)
	}
	exclude, err := compileOptionalRegexp(extension.Exclude, "_exclude")
	if err != nil {
		return fmt.Errorf("outbounds[%d]: %w", index, err)
	}
	includeRegex, err := compileOptionalRegexp(extension.Include, "_include")
	if err != nil {
		return fmt.Errorf("outbounds[%d]: %w", index, err)
	}
	existing, err := stringArrayField(object, "outbounds")
	if err != nil {
		return fmt.Errorf("outbounds[%d]: %w", index, err)
	}
	selectedProviders := extension.Providers
	if extension.UseAllProvider {
		selectedProviders = selectedProviders[:0]
		for _, provider := range providers {
			selectedProviders = append(selectedProviders, provider.tag)
		}
	}
	for _, providerTag := range selectedProviders {
		provider, found := providerByTag[providerTag]
		if !found {
			return fmt.Errorf("outbounds[%d]: provider %q not found", index, providerTag)
		}
		for _, tag := range provider.tags {
			if matchesFilters(tag, exclude, includeRegex) {
				existing = append(existing, tag)
			}
		}
	}
	if len(existing) == 0 {
		return fmt.Errorf("outbounds[%d]: group has no outbounds after provider expansion", index)
	}
	for _, tag := range existing {
		if !validTags[tag] {
			return fmt.Errorf("outbounds[%d]: referenced outbound or endpoint %q not found", index, tag)
		}
	}
	if defaultTag, found := stringField(object, "default"); found && defaultTag != "" {
		present := false
		for _, tag := range existing {
			present = present || tag == defaultTag
		}
		if !present {
			return fmt.Errorf("outbounds[%d]: default outbound %q is not in expanded outbounds", index, defaultTag)
		}
	}
	object.Put("outbounds", stringsToJSONArray(existing))
	object.Remove("_providers")
	object.Remove("_exclude")
	object.Remove("_include")
	object.Remove("_use_all_providers")
	return nil
}

func stringField(object *badjson.JSONObject, key string) (string, bool) {
	value, found := object.Get(key)
	if !found {
		return "", false
	}
	valueString, ok := value.(string)
	return valueString, ok
}

func stringArrayField(object *badjson.JSONObject, key string) ([]string, error) {
	value, found := object.Get(key)
	if !found {
		return nil, nil
	}
	array, ok := value.(badjson.JSONArray)
	if !ok {
		return nil, fmt.Errorf("%s must be an array", key)
	}
	result := make([]string, 0, len(array))
	for index, item := range array {
		itemString, ok := item.(string)
		if !ok {
			return nil, fmt.Errorf("%s[%d] must be a string", key, index)
		}
		result = append(result, itemString)
	}
	return result, nil
}

func stringsToJSONArray(values []string) badjson.JSONArray {
	array := make(badjson.JSONArray, len(values))
	for index, value := range values {
		array[index] = value
	}
	return array
}

func outboundObject(ctx context.Context, outbound option.Outbound) (*badjson.JSONObject, error) {
	content, err := SJSON.MarshalContext(ctx, &outbound)
	if err != nil {
		return nil, fmt.Errorf("encode generated outbound %q: %w", outbound.Tag, err)
	}
	var object badjson.JSONObject
	if err = object.UnmarshalJSONContext(ctx, content); err != nil {
		return nil, fmt.Errorf("decode generated outbound %q: %w", outbound.Tag, err)
	}
	return &object, nil
}

func endpointObject(ctx context.Context, endpoint option.Endpoint) (*badjson.JSONObject, error) {
	content, err := SJSON.MarshalContext(ctx, &endpoint)
	if err != nil {
		return nil, fmt.Errorf("encode generated endpoint %q: %w", endpoint.Tag, err)
	}
	var object badjson.JSONObject
	if err = object.UnmarshalJSONContext(ctx, content); err != nil {
		return nil, fmt.Errorf("decode generated endpoint %q: %w", endpoint.Tag, err)
	}
	return &object, nil
}
