# Local patch for gopkg.in/yaml.v3 v3.0.1

`emitterc.go` resets the empty event queue so later emissions reuse its backing array.
