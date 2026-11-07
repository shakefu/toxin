// Package live is a scanner test fixture for --load-configs.
package live

func live() {
	toxin.Get("live.set", "fallback")
	toxin.Get("live.falsy", "fallback")
	toxin.Get("live.falsynodefault")
	toxin.Get("live.absent")
}
