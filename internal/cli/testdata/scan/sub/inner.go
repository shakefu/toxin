// Package inner is a scanner test fixture below the scan root.
package inner

func inner() {
	toxin.Get("inner.key", "inner default")
	toxin.Get("sample.plain")
}
