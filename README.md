# Kilo OAuth plugin for CLIProxyAPI

This native plugin uses the Kilo account device authorization flow. The CLIProxy host owns outbound HTTP, request cancellation and streaming. The plugin discovers the catalog for the authorized account and its selected organization; it does not substitute anonymous or static models.

The SDK is pinned to official CLIProxyAPI `v8.0.70` (`905e70c48b47b7a1cb4a27dd3abc901e2cdd1e4a`). A host that exposes the additive public `UserCode` device-flow field is required for interactive management login. Official `v8.0.70` alone can load the library but does not display the public code; use a host release that includes [upstream PR #7549](https://github.com/router-for-me/CLIProxyAPI/pull/7549) or its equivalent. This plugin cannot use the `no-plugin` build.

Build and test:

```sh
go test ./...
go build -buildmode=c-shared -o kilo.so ./go
```

Place `kilo.so` in the configured CLIProxy plugin directory, enable plugin ID `kilo`, and verify registration through `/v8/management/plugins`. For accounts with multiple organizations, supply the intended `organization_id` during device authorization. The plugin does not choose among multiple organizations. Renewal requires another authorization when the gateway no longer accepts the account token.

The test suite uses synthetic accounts. It does not establish that a particular live account or organization is eligible for inference.
