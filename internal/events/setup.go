package events

import "github.com/ideacrafterslabs/ctxt/internal/config"

// SetupSubscriber registers the profile auto-provisioning subscriber on the bus.
// Call after bus creation in serve startup. cfgPath is the path to config.yaml
// for write-back on profile mutations.
func SetupSubscriber(b Bus, cfg *config.Config, cfgPath string) *Subscriber {
	sub := NewSubscriber(cfg, cfgPath)
	sub.Register(b)
	return sub
}
