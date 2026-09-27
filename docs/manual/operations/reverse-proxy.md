# Put dpkms Behind a Reverse Proxy

Use this page to open the web UI and the API of a remote dpkms in a
browser. A reverse proxy in front of dpkms signs you in, terminates TLS,
and adds the instance's token to every request it forwards. dpkms itself
needs no new feature: it runs as a protected instance with one static
token.

```text
browser ──HTTPS + sign-in──▶ proxy ──HTTP + Authorization: Bearer <token>──▶ dpkms (protected)
```

## Is this the right setup?

Use it when one person, or a group that may share everything, reaches a
remote instance from a browser, and either basic auth is enough or you
already run a sign-in proxy (oauth2-proxy, Authelia, Cloudflare Access,
Tailscale Serve).

Know what it means before you set it up:

- **Everyone the proxy lets through is the same principal.** dpkms sees
  one token, so every user gets the same data, roles and quotas, and
  dpkms logs and security events cannot tell them apart.
- **The proxy is the authentication.** It adds the token to anything it
  forwards, so whoever gets past the proxy's sign-in is signed in to
  dpkms. Never run it without a sign-in step or a network limit such as a
  tailnet.
- **Non-browser clients stay outside it.** The ctxt CLI and agents keep
  their own token and talk to dpkms directly (see
  [Clients other than the browser](#clients-other-than-the-browser)).

dpkms does not have its own browser sign-in yet. One is planned; until
it ships, a proxy is the way to use the web UI on a protected or public
instance. If people need separate identities, give each one their own
CLI token instead.

Never put a **private** instance behind a proxy: it has no
authentication at all. See
[Never proxy a private instance](#never-proxy-a-private-instance).

## 1. Make dpkms a protected instance

Generate a long random token:

```bash
openssl rand -hex 32
```

Add it to the dpkms config file (`dpkms config paths` lists where dpkms
looks, for example `~/.config/contexthelp/dpkms.yaml`), together with the
proxy's public host name:

```yaml
server:
  access: protected
  allowed_hosts:
    - dpkms.example.com
  auth:
    provider: static
    static:
      tokens:
        - token: <token>
          principal: me
          roles: [admin]
```

Restart dpkms:

```bash
dpkms serve --port 8080
```

It prints, among other lines:

```text
Inbound auth: static provider (protected access)
HTTP server listening on 0.0.0.0:8080
```

A protected instance without a token refuses to start with
`protected instance requires inbound authentication`.

`server.allowed_hosts` switches on the `Host` check, which stops a web
page that points its own DNS name at your server from using the API.
Protected and public instances check `Host` only when the list is set,
so set it. `127.0.0.1:<port>` and `localhost:<port>` stay allowed for
local clients.

Write the name without a port. dpkms receives plain HTTP from the proxy,
so it reads a `Host` without a port as port 80: an entry such as
`dpkms.example.com:443` never matches and every request gets `403`
`HOST_NOT_ALLOWED`. The entry syntax is in
[Reach dpkms by another host name](../reference/config-and-permissions.md#reach-dpkms-by-another-host-name).

## 2. Let only the proxy reach dpkms

A protected instance listens on every interface, for HTTP (`8080`) and
gRPC (`9090`), and has no setting to bind a single address. Anyone who
reaches port 8080 without the proxy only needs the token, so close it
with a firewall.

With the proxy on the same host, block both ports from outside. A
same-host proxy connects over loopback, which ufw does not filter:

```bash
sudo ufw deny 8080/tcp
sudo ufw deny 9090/tcp
```

With the proxy on another host (here `10.0.0.2`), allow it before the
deny rule, since ufw applies the first rule that matches:

```bash
sudo ufw allow from 10.0.0.2 to any port 8080 proto tcp
sudo ufw deny 8080/tcp
sudo ufw deny 9090/tcp
```

Leave 9090 open to the CLI clients and agents that use gRPC if you have
any: they authenticate with their own token.

If dpkms runs in Docker, publish its port on loopback only
(`127.0.0.1:8080:8080`): ports Docker publishes skip ufw's rules.

Check from another machine: `curl -m 5 http://<dpkms-host>:8080/health`
must time out or be refused.

## 3. Configure the proxy

Both examples do the same four things:

- sign the user in (basic auth here; swap in your sign-in proxy),
- terminate TLS,
- replace any `Authorization` header the client sent with
  `Authorization: Bearer <token>`, and drop any `X-API-Key`,
- keep the `Host` header the browser sent.

Pick one.

### Caddy

Hash a password for the sign-in (it prompts for it):

```bash
caddy hash-password
```

`/etc/caddy/Caddyfile`, with your hash in place of the sample one:

```caddyfile
dpkms.example.com {
	basic_auth {
		jad $2a$14$8x.L2aNURd4kTz6T1W8.1eNDVHiJp3oI0PXdENz.kLo/so0qWEHme
	}

	reverse_proxy 127.0.0.1:8080 {
		header_up Authorization "Bearer {env.DPKMS_PROXY_TOKEN}"
		header_up -X-API-Key
	}
}
```

Give Caddy the token through its environment, not the Caddyfile. With
the packaged systemd service, run `sudo systemctl edit caddy` and add
`Environment=DPKMS_PROXY_TOKEN=<token>` under `[Service]`, then restart
Caddy.

What each part does:

- Caddy fetches and renews the certificate for `dpkms.example.com` on its
  own.
- `header_up Authorization` replaces the header, including the basic-auth
  credentials the browser just sent to Caddy. `header_up -X-API-Key`
  removes that header.
- Caddy keeps the browser's `Host` by default and streams live updates
  and WebSockets with no extra settings.
- For single sign-on, replace `basic_auth` with Caddy's
  [`forward_auth`](https://caddyserver.com/docs/caddyfile/directives/forward_auth)
  pointed at your sign-in proxy.

### nginx

Create the sign-in user (`htpasswd` comes with `apache2-utils` or
`httpd-tools`; it prompts for the password):

```bash
sudo htpasswd -B -c /etc/nginx/dpkms.htpasswd jad
```

Keep the token in a file only root can read:

```bash
sudo install -m 600 /dev/null /etc/nginx/dpkms-token.conf
sudoedit /etc/nginx/dpkms-token.conf
```

It holds one line:

```nginx
proxy_set_header Authorization "Bearer <token>";
```

`/etc/nginx/conf.d/dpkms.conf`:

```nginx
map $http_upgrade $connection_upgrade {
    default upgrade;
    ''      close;
}

server {
    listen 80;
    server_name dpkms.example.com;
    return 301 https://$host$request_uri;
}

server {
    listen 443 ssl;
    server_name dpkms.example.com;

    ssl_certificate     /etc/letsencrypt/live/dpkms.example.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/dpkms.example.com/privkey.pem;

    auth_basic           "dpkms";
    auth_basic_user_file /etc/nginx/dpkms.htpasswd;

    proxy_http_version 1.1;
    proxy_set_header Host $http_host;
    proxy_set_header X-API-Key "";
    proxy_set_header Upgrade $http_upgrade;
    proxy_set_header Connection $connection_upgrade;
    include /etc/nginx/dpkms-token.conf;

    location / {
        proxy_pass http://127.0.0.1:8080;
    }

    location = /api/v1/events {
        proxy_pass http://127.0.0.1:8080;
        proxy_read_timeout 1h;
    }
}
```

Test and reload with `sudo nginx -t && sudo systemctl reload nginx`.

What each part does:

- The `proxy_set_header` lines sit at `server` level on purpose: a
  `location` that sets any `proxy_set_header` of its own loses all the
  inherited ones, token included.
- `Host $http_host` keeps the name and port the browser used. nginx's
  default sends the upstream address instead (`127.0.0.1:8080`).
- The token line replaces the basic-auth `Authorization` header;
  `X-API-Key ""` drops that header.
- `/api/v1/events` is the live-update stream. dpkms sends
  `X-Accel-Buffering: no`, so nginx does not buffer it, but nginx closes
  a stream that stays quiet for 60 seconds unless `proxy_read_timeout`
  is raised.
- `Upgrade` and `Connection` pass WebSocket handshakes through to
  `/ws/bus`.
- The certificate paths are certbot's; use your own.
- For single sign-on, replace `auth_basic` with nginx's
  [`auth_request`](https://nginx.org/en/docs/http/ngx_http_auth_request_module.html)
  pointed at your sign-in proxy.

## 4. Check it

```bash
curl -u jad https://dpkms.example.com/api/v1/objects
```

It prompts for the password and prints the objects as JSON. Without
`-u jad` the proxy answers `401` before dpkms sees the request.

Then open `https://dpkms.example.com/ui/`. The web UI loads and can
delete objects and retry jobs, because its requests come from the
instance's own origin.

## What passes through the proxy

**Writes must be JSON.** A `POST`, `PATCH`, `PUT` or `DELETE` with a body
needs `Content-Type: application/json`, or dpkms answers `415`
`UNSUPPORTED_MEDIA_TYPE`. The web UI already sends it; add it to scripts:

```bash
curl -u jad -X POST https://dpkms.example.com/api/v1/inbox \
  -H 'Content-Type: application/json' \
  -d '{"content":"remember this"}'
```

**Other sites cannot write.** The proxy adds the token to anything that
passes its sign-in, including a request another site's page makes while
your sign-in cookie is still valid. dpkms refuses such writes itself:
a write whose `Sec-Fetch-Site` or `Origin` header names another site
gets `403` `CROSS_ORIGIN_REQUEST`. Other sites cannot read responses
either, because dpkms sends no CORS headers to remote host names. Do not
add CORS headers at the proxy.

**Live updates and WebSockets.** `GET /api/v1/events` is a Server-Sent
Events stream: Caddy passes it as is, nginx needs the longer read
timeout shown above. `/ws/bus` is the event-bus WebSocket for other
tools. It authenticates with the bus token in its first message, not with
the header the proxy adds, so the proxy only has to let the upgrade
through.

**Client addresses.** dpkms sees the proxy as the client and does not
read `X-Forwarded-For`, so failed sign-ins in dpkms's security events
all come from the proxy's address. The proxy's own log has the real
client address.

### Clients other than the browser

The proxy's sign-in step stands between the ctxt CLI and dpkms. Point
the CLI at dpkms directly (over a VPN, a tailnet or an SSH tunnel) with
its own token in `server.url` and `server.token`, or in a `server.urls`
entry. Give each machine its own entry under `server.auth.static.tokens`
so you can revoke one without the others.

## Never proxy a private instance

A private instance (the default) authenticates nobody and always answers
to `127.0.0.1:<port>`. A proxy on the same host reaches it over
loopback, and nginx's default `Host` for `proxy_pass
http://127.0.0.1:8080` is exactly `127.0.0.1:8080`. The whole API is then
open to anyone who reaches the proxy, and the token the proxy adds is
ignored.

If a private instance behind a proxy answers `403` `HOST_NOT_ALLOWED`,
do not add the proxy's name to `server.allowed_hosts`. Make the instance
protected, as in step 1.

To use a private instance on a remote machine, tunnel to it instead:

```bash
ssh -L 8080:127.0.0.1:8080 <dpkms-host>
```

and open `http://localhost:8080/ui/`.

## Troubleshooting

### `403 HOST_NOT_ALLOWED`

The `Host` the proxy forwards is not in `server.allowed_hosts`. The error
message quotes the host dpkms received.

- The name is missing, or it has a port: list `dpkms.example.com`, not
  `dpkms.example.com:443`.
- nginx without `proxy_set_header Host $http_host` sends the upstream
  address (`10.0.0.5:8080`). Add the line.
- A load balancer health check that uses an IP address as `Host` gets
  the same answer. Probe `/health` on `127.0.0.1:8080` from the dpkms
  host, or list the address.

### `401 UNAUTHORIZED`

Check who answered. dpkms answers with a JSON body and
`WWW-Authenticate: Bearer realm="dpkms"`; a proxy's sign-in failure does
not.

- `"authentication required"`: the proxy did not add the token. With
  Caddy, `DPKMS_PROXY_TOKEN` is not set in Caddy's environment. With
  nginx, the `include` line is missing or sits inside a `location` that
  has its own `proxy_set_header`.
- `"invalid credentials"`: the proxy's token differs from every
  `server.auth.static.tokens` entry. Copy it again and restart dpkms after
  editing its config.

### `415 UNSUPPORTED_MEDIA_TYPE`

The write had a body that was not labelled JSON. Send
`Content-Type: application/json`.

### `403 CROSS_ORIGIN_REQUEST`

A browser write came from a page on another site or port. Open the web
UI through the proxy's own address. If the UI itself gets this error
through nginx, check `proxy_set_header Host $http_host`: an old browser
that sends only `Origin` is compared against the forwarded `Host`.

### Live updates stop or never arrive

- nginx closes a quiet stream after 60 seconds: keep the
  `location = /api/v1/events` block with `proxy_read_timeout 1h`. A stream
  that sees no event at all before the timeout ends with `504`.
- Another layer that buffers responses (a CDN, a second proxy) must pass
  `text/event-stream` through unbuffered.

### `502 Bad Gateway`

The proxy cannot reach dpkms: dpkms is not running, listens on another
port, or a firewall rule from step 2 blocks the proxy's host. When the
port it was given is taken, dpkms starts on a free one instead; its
`HTTP server listening on` line shows the port it really uses.

## Related

- [Config and Permissions](../reference/config-and-permissions.md): `Host`
  check, JSON-only writes and cross-origin rules
- [Runbook](./runbook.md)
