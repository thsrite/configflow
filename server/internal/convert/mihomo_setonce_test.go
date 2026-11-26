package convert

import (
	"errors"
	"testing"

	"configflow/internal/model"
)

func TestSetMihomoEnvironmentSetOnce(t *testing.T) {
	environmentState.Lock()
	oldVal := environmentState.value
	oldSet := environmentState.set
	environmentState.value = MihomoEnvironment{Clients: defaultClientCollaborators()}
	environmentState.set = false
	environmentState.Unlock()
	t.Cleanup(func() {
		environmentState.Lock()
		environmentState.value = oldVal
		environmentState.set = oldSet
		environmentState.Unlock()
	})

	first := MihomoEnvironment{
		ProviderInterval: func(model.MihomoSettings) (int64, error) { return 42, nil },
	}
	if err := SetMihomoEnvironment(first); err != nil {
		t.Fatalf("first SetMihomoEnvironment returned unexpected error: %v", err)
	}

	second := MihomoEnvironment{
		ProviderInterval: func(model.MihomoSettings) (int64, error) { return 99, nil },
	}
	if err := SetMihomoEnvironment(second); !errors.Is(err, ErrMihomoEnvironmentAlreadySet) {
		t.Fatalf("second SetMihomoEnvironment error = %v, want %v", err, ErrMihomoEnvironmentAlreadySet)
	}

	got, err := CurrentMihomoEnvironment().ProviderInterval(model.MihomoSettings{})
	if err != nil || got != 42 {
		t.Fatalf("environment was overwritten after second call: got (%d, %v), want (42, nil)", got, err)
	}
}

func TestSetMihomoIntegrationsSetOnce(t *testing.T) {
	integrationState.Lock()
	oldVal := integrationState.value
	oldSet := integrationState.set
	integrationState.value = MihomoIntegrations{}
	integrationState.set = false
	integrationState.Unlock()
	t.Cleanup(func() {
		integrationState.Lock()
		integrationState.value = oldVal
		integrationState.set = oldSet
		integrationState.Unlock()
	})

	first := MihomoIntegrations{
		GatewayEnabled: func(*model.Config) (bool, error) { return true, nil },
	}
	if err := SetMihomoIntegrations(first); err != nil {
		t.Fatalf("first SetMihomoIntegrations returned unexpected error: %v", err)
	}

	second := MihomoIntegrations{
		GatewayEnabled: func(*model.Config) (bool, error) { return false, nil },
	}
	if err := SetMihomoIntegrations(second); !errors.Is(err, ErrMihomoIntegrationsAlreadySet) {
		t.Fatalf("second SetMihomoIntegrations error = %v, want %v", err, ErrMihomoIntegrationsAlreadySet)
	}

	got, err := currentIntegrations().GatewayEnabled(&model.Config{})
	if err != nil || !got {
		t.Fatalf("integrations were overwritten after second call: got (%v, %v), want (true, nil)", got, err)
	}
}
