package mihomoenv

import (
	"reflect"
	"testing"

	"configflow/internal/convert"
)

func TestProductionWireRegistersEveryCallback(t *testing.T) {
	t.Cleanup(convert.SetMihomoEnvironment(convert.MihomoEnvironment{}))
	Wire()
	env := convert.CurrentMihomoEnvironment()
	if err := env.Clients.Check(); err != nil {
		t.Fatal(err)
	}
	if err := env.CheckGeneration(); err != nil {
		t.Fatal(err)
	}
	value := reflect.ValueOf(env)
	for i := range value.NumField() {
		field := value.Field(i)
		if (field.Kind() == reflect.Func || field.Kind() == reflect.Interface) && field.IsNil() {
			t.Errorf("missing production callback %s", value.Type().Field(i).Name)
		}
	}
}
