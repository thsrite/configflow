package oxidns

import "errors"

// yamlReadError retains the shared policy/alias cause and the ordinary settings
// error classification at the HTTP boundary. It never turns a policy refusal
// into the syntax-only template fallback.
type yamlReadError struct {
	*SettingsError
	cause error
}

func (e *yamlReadError) Unwrap() []error { return []error{e.SettingsError, e.cause} }

func yamlReaderFailure(err error) error {
	return &yamlReadError{SettingsError: &SettingsError{Msg: err.Error()}, cause: err}
}

// yamlReaderContext retains the unchanged include diagnostic and its source
// metadata while preserving the reader cause through that contextual wrapper.
func yamlReaderContext(err error, source string, at Path) error {
	context := structureError(err.Error(), source, at)
	var read *yamlReadError
	var settings *SettingsError
	if errors.As(err, &read) && errors.As(context, &settings) {
		return &yamlReadError{SettingsError: settings, cause: err}
	}
	return context
}
