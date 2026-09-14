package gateway

import (
	"github.com/a3tai/openclaw-go/identity"
	"github.com/a3tai/openclaw-go/protocol"
)

// WithIdentity configures the client to authenticate using a device keypair.
// If deviceToken is non-empty, it overrides the bearer token for this connect
// (the device uses its issued token instead of the shared gateway token).
// The identity is used to sign the v2 device-auth payload.
func WithIdentity(id *identity.Identity, deviceToken string) Option {
	return func(o *options) {
		if deviceToken != "" {
			o.token = deviceToken
		}
		o.deviceSigner = func(p identity.SigningParams) *protocol.DeviceIdentity {
			proto := id.BuildDeviceIdentity(p)
			return &protocol.DeviceIdentity{
				ID:        proto.ID,
				PublicKey: proto.PublicKey,
				Signature: proto.Signature,
				SignedAt:  proto.SignedAt,
				Nonce:     proto.Nonce,
			}
		}
	}
}
