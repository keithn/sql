package db

import "fmt"

var drivers = map[string]Driver{}

// Register makes a driver available by name. Called from driver package init().
func Register(name string, d Driver) {
	if _, exists := drivers[name]; exists {
		panic(fmt.Sprintf("db: driver %q already registered", name))
	}
	drivers[name] = d
}

// Get returns the driver registered under name, or an error if not found.
func Get(name string) (Driver, error) {
	d, ok := drivers[name]
	if !ok {
		return nil, fmt.Errorf("db: no driver registered for %q", name)
	}
	return d, nil
}

// Names returns all registered driver names.
func Names() []string {
	names := make([]string, 0, len(drivers))
	for n := range drivers {
		names = append(names, n)
	}
	return names
}
