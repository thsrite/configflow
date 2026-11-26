package trafficstore

import (
	"errors"
	"fmt"
)

// ErrInvalid reports a traffic report or stored traffic file whose members
// have the wrong type; a stored file failing this way is marked damaged.
var ErrInvalid = errors.New("invalid traffic data")

// ErrMissingMember reports a required member that is absent.
var ErrMissingMember = errors.New("missing traffic member")

func invalidData(detail string) error { return fmt.Errorf("%w: %s", ErrInvalid, detail) }

func missingMember(name string) error { return fmt.Errorf("%w %q", ErrMissingMember, name) }
