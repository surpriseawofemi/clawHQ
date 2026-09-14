# Security Policy

## Supported Versions

| Version | Supported |
|---------|-----------|
| latest  | Yes       |

## Reporting a Vulnerability

If you discover a security vulnerability in openclaw-go, please report it
responsibly. **Do not open a public issue.**

Email: **security@a3t.ai**

Include:
- Description of the vulnerability
- Steps to reproduce
- Potential impact
- Suggested fix (if any)

## Response Timeline

- **Acknowledgment**: Within 48 hours
- **Assessment**: Within 1 week
- **Fix or mitigation**: Depending on severity, typically within 2 weeks

We will coordinate disclosure with you and credit you in the advisory unless
you prefer to remain anonymous.

## Scope

This policy covers the `openclaw-go` library code. Issues in the upstream
OpenClaw gateway or protocol should be reported to the OpenClaw project directly.

## Security Best Practices for Users

- Always use TLS (`wss://` / `https://`) in production
- Store tokens and passwords securely -- never commit them to source control
- Use the shortest-lived tokens possible
- Set appropriate timeouts via `WithConnectTimeout`
- Validate and sanitize any user input before passing it to gateway RPCs
