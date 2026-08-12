package config_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/config"
)

// TestSensorToggleCarriesOnlyTheSharedKey is a LOCK, not a description.
//
// RF-10's test_severity is a knob only the security sensor honours. The
// tempting one-line edit — put TestSeverity on SensorToggle so
// Sensors.Security keeps its old type — makes `sensors.db.test_severity`,
// `sensors.tests.test_severity` and three more PARSE, VALIDATE and do
// absolutely nothing. That is not a style preference: it is new dead config,
// the same defect class as Report.IncludeInfo, in a tool whose whole premise is
// catching what the developer never sees. Go cannot forbid the field, so this
// test is the control.
//
// The failure must be readable enough to stop the edit: it names every sensor
// that just silently gained the key.
//
// It carries its own positive probe. A census that walked nothing would pass
// forever, so the test first proves it FOUND the shared-toggle sensors and that
// Sensors.Security is deliberately NOT one of them.
func TestSensorToggleCarriesOnlyTheSharedKey(t *testing.T) {
	toggleType := reflect.TypeOf(config.SensorToggle{})
	sensorsType := reflect.TypeOf(config.Sensors{})

	var sharing []string
	securityIsShared := false
	for i := range sensorsType.NumField() {
		f := sensorsType.Field(i)
		if f.Type != toggleType {
			continue
		}
		sharing = append(sharing, yamlKey(f))
		if f.Name == "Security" {
			securityIsShared = true
		}
	}

	// Positive probe #1: the census actually resolved sensors of the shared
	// type. Zero would mean the reflection walk is broken (renamed type, moved
	// package) and every assertion below would be vacuous.
	if len(sharing) == 0 {
		t.Fatalf("census found no %s-typed field on config.Sensors — the walk is broken, "+
			"so this lock guards nothing", toggleType.Name())
	}
	// Positive probe #2: the whole point of SecuritySensor is that Security is
	// NOT on the shared type. If it were, TestSeverity would have nowhere to
	// live but the shared struct and this lock would be unsatisfiable.
	if securityIsShared {
		t.Errorf("Sensors.Security is typed %s again — RF-10's test_severity has no "+
			"security-specific home, so it can only be added to the shared type",
			toggleType.Name())
	}

	var carried []string
	for i := range toggleType.NumField() {
		carried = append(carried, toggleType.Field(i).Name)
	}
	if len(carried) != 1 || carried[0] != "Enabled" {
		t.Errorf("%s must carry exactly {Enabled}, got {%s}.\n"+
			"A sensor-specific key on the shared toggle is accepted and ignored by "+
			"these %d sensors, which read none of it: %s.\n"+
			"Put the knob on the owning sensor's own type (see config.SecuritySensor).",
			toggleType.Name(), strings.Join(carried, ", "),
			len(sharing), strings.Join(sharing, ", "))
	}
}

// yamlKey returns the field's yaml key, falling back to the Go field name.
func yamlKey(f reflect.StructField) string {
	tag := f.Tag.Get("yaml")
	if name, _, _ := strings.Cut(tag, ","); name != "" {
		return name
	}
	return f.Name
}
