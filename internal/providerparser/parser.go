package providerparser

import (
	"context"

	"github.com/sagernet/sing-box/option"
	E "github.com/sagernet/sing/common/exceptions"
)

var subscriptionParsers = []func(context.Context, string) ([]option.Outbound, []option.Endpoint, error){
	ParseClashSubscription,
	ParseSIP008Subscription,
	ParseRawSubscription,
}

// ParseSubscription preserves the public shape used by the reF1nd parser. The
// override arguments are intentionally ignored: this converter does not offer
// provider dialer or TLS overrides.
func ParseSubscription(ctx context.Context, content string, _, _ any, providerTag string) ([]option.Outbound, []option.Endpoint, error) {
	var parseErr error
	for _, parser := range subscriptionParsers {
		outbounds, endpoints, err := parser(ctx, content)
		if len(outbounds) > 0 || len(endpoints) > 0 {
			rewriteDetours(outbounds, endpoints, providerTag)
			return outbounds, endpoints, nil
		}
		parseErr = E.Errors(parseErr, err)
	}
	return nil, nil, E.Cause(parseErr, "no servers found")
}

func rewriteDetours(outbounds []option.Outbound, endpoints []option.Endpoint, providerTag string) {
	known := make(map[string]bool, len(outbounds)+len(endpoints))
	for _, outbound := range outbounds {
		known[outbound.Tag] = true
	}
	for _, endpoint := range endpoints {
		known[endpoint.Tag] = true
	}
	rewrite := func(options any) {
		wrapper, ok := options.(option.DialerOptionsWrapper)
		if !ok {
			return
		}
		dialer := wrapper.TakeDialerOptions()
		if dialer.Detour == "" {
			return
		}
		if known[dialer.Detour] && providerTag != "" {
			dialer.Detour = providerTag + "/" + dialer.Detour
		} else if !known[dialer.Detour] {
			dialer.Detour = ""
		}
		wrapper.ReplaceDialerOptions(dialer)
	}
	for _, outbound := range outbounds {
		rewrite(outbound.Options)
	}
	for _, endpoint := range endpoints {
		rewrite(endpoint.Options)
	}
}
