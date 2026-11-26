package metrics

import "errors"

// ErrTrafficSettingsShape is returned when the stored system settings are not
// an object, so the traffic settings cannot be read or written.
var ErrTrafficSettingsShape = errors.New(msgTrafficSettingsShape)

// errCounterInvalid is a stored traffic counter that is not a finite number.
var errCounterInvalid = errors.New(msgTrafficCounterInvalid)

// errCounterRange is a traffic counter SQLite cannot store as INTEGER or REAL.
var errCounterRange = errors.New(msgTrafficCounterRange)
