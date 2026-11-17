package installer

import _ "embed"

//go:embed ha-startup-checks.sh
var haStartupChecks string

func haPortCheck(image string) string {
	return "if [ -z \"$cf_main_id\" ] || [ \"$(docker inspect --format '{{.State.Running}}' \"$cf_main_id\")\" != true ]; then\n  cf_check_dns_port " + q(image) + "\nfi\n"
}

func haNewReadiness(name string) string {
	return "cf_wait_oxidns \"$(cf_owned_container main " + q(name) + ")\"\n"
}

const haMigrationReadiness = "cf_wait_oxidns \"$(cat \"$cf_backup/new-id\")\"\n"
