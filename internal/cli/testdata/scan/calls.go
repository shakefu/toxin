// Package calls is a scanner test fixture: toxin calls of many shapes.
package calls

func work() {
	toxin.Get("sample.plain")
	toxin.Get("sample.dedup")
	toxin.Get("sample.dedup", "fallback")
	toxin.Get("sample.withdefault", "default value")
	toxin.Set("sample.num", 10)
	toxin.GetStrict("sample.strict", true)
	toxin.Get(keyVar)
	toxin.Get(config.Keys.sample)
	toxin.Get("sample." + suffix)
	toxin.Get("sample.unparsed", someFunc())
	toxin.Get("sample.multi", 1, "two", false)
	toxin.Get("sample.composite", []string{"a", "b"})
	toxin.Get("sample.map", map[string]int{"a": 1})
	toxin.Get("sample.neg", -1)
	toxin.Get("sample.nil", nil)
	other.Get("sample.other")
	toxin.Other("sample.other")
	toxin.Get()
}
