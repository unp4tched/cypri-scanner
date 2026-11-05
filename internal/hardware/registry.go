package hardware

import (
	"golang.org/x/sys/windows/registry"
)

// registryString reads a string value from the registry.
func registryString(root registry.Key, path, name string) (string, bool) {
	key, err := registry.OpenKey(root, path, registry.QUERY_VALUE|registry.WOW64_64KEY)
	if err != nil {
		return "", false
	}
	defer key.Close()

	value, _, err := key.GetStringValue(name)
	if err != nil {
		return "", false
	}
	return value, true
}
