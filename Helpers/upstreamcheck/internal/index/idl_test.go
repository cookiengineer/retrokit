package index

import "testing"

func TestIndexIDL(t *testing.T) {
	src := []byte(`
[Exposed=Window]
interface Geolocation {
    readonly attribute double latitude;
    undefined getCurrentPosition(PositionCallback successCallback);
};

dictionary PositionOptions {
    boolean enableHighAccuracy = false;
};

enum GeolocationError {
    "permission-denied",
    "position-unavailable"
};
`)
	symbols := IndexIDL("Geolocation.idl", src)
	got := make(map[string]string)
	for _, sym := range symbols {
		got[sym.Name] = sym.Kind
	}

	expected := map[string]string{
		"Geolocation":                         "interface",
		"Geolocation::latitude":               "idl_attribute",
		"Geolocation::getCurrentPosition":     "idl_operation",
		"PositionOptions":                     "dictionary",
		"PositionOptions::enableHighAccuracy": "idl_member",
		"GeolocationError":                    "enum",
		"GeolocationError::permission-denied": "enum_value",
	}
	for name, kind := range expected {
		if got[name] != kind {
			t.Errorf("symbol %q: got kind %q, want %q\nall symbols: %#v", name, got[name], kind, got)
		}
	}
}
