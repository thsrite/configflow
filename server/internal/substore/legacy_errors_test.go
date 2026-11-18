package substore

import "fmt"

type PyError struct {
	Class, Message string
	Err            error
}

func (e *PyError) Error() string           { return e.Message }
func (e *PyError) Unwrap() error           { return e.Err }
func pyErr(class, message string) *PyError { return &PyError{Class: class, Message: message} }
func pythonClass(err error) string {
	switch value := err.(type) {
	case *PyError:
		return value.Class
	case *RequestError:
		return value.Class
	case *RuntimeUnavailable:
		return "RuntimeUnavailable"
	case *DependencyUpdateError:
		return "DependencyUpdateError"
	}
	return fmt.Sprintf("%T", err)
}
