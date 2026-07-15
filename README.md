# subsing

`subsing` 将带有 provider 扩展字段的伪 sing-box 配置转换成原版 sing-box 配置。它只执行一次下载和展开，不提供缓存、定时更新、下载路由、健康检查或拨号/TLS 覆写。

## 构建和运行

```bash
make build
./subsing <输入目录> <输出目录>
```

也可以使用 Docker：

```bash
docker build -t subsing:latest .
docker run --rm \
  --volume ./input/config.json.template:/workdir/config.json \
  --volume ./output:/processed \
  subsing:latest
```

程序只处理输入目录当前层的 `.json`、`.jsonc` 普通文件。输出目录不存在或为空时，所有文件转换成功后会整体发布；输出目录非空时视为已经生成过，直接成功跳过。输入与输出目录不能相同、互相包含或经符号链接发生重叠。

## 扩展配置

顶层远程 provider：

```json
{
  "_providers": [
    {
      "type": "remote",
      "tag": "airport",
      "url": "https://example.com/subscription",
      "exclude": "到期|流量",
      "include": "香港|日本",
      "user_agent": "clash.meta"
    }
  ]
}
```

`type` 必须为 `remote`，`tag` 必须非空且在配置内唯一，`url` 必须为绝对 HTTP(S) URL。`exclude/include` 是 Go 正则表达式，匹配订阅中的原始节点名。

selector/urltest 引用 provider：

```json
{
  "type": "selector",
  "tag": "proxy",
  "outbounds": ["direct"],
  "_providers": ["airport"],
  "_exclude": "实验",
  "_include": "airport/",
  "_use_all_providers": false
}
```

节点最终命名为 `providerTag/nodeName`，重复名称依次添加 ` (2)`、` (3)`。普通节点追加到顶层 `outbounds`，WireGuard/Tailscale 追加到顶层 `endpoints`，组的 `outbounds` 会引用相应 tag。输出时会删除所有已识别的下划线扩展字段。转换器会检查扩展字段、标签冲突和组引用，但不会加载完整 sing-box 协议实现来校验最终配置；需要时可另行运行 `sing-box check -c <配置文件>`。

支持 Clash/Mihomo YAML、SIP008、明文或 Base64 URI 列表，并额外以原始对象方式处理 sing-box JSON 订阅。JSONC 注释和原排版不会保留。
