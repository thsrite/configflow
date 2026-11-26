package mihomoenv

import (
	"reflect"
	"testing"

	"configflow/internal/convert"
	"configflow/internal/provider"
	"configflow/internal/substore"
)

func TestProductionWireRegistersEveryCallback(t *testing.T) {
	t.Cleanup(convert.ResetMihomoEnvironmentForTest(t))
	if err := WireWith(nil, substore.NewService(substore.NewEnv()), provider.New(provider.Options{})); err != nil {
		t.Fatal(err)
	}
	env := convert.CurrentMihomoEnvironment()
	if env.Clients.ParseNodes == nil || env.Clients.FilterNodes == nil {
		t.Fatal("missing production client callback")
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
