// Package settings holds the app-settings whitelist consumed by both the public
// config endpoint and the admin API. Only overrides are persisted (in
// coc_app_settings); defaults live here so both handlers agree on shape/type.
package settings

import (
	"encoding/json"
	"math"
	"strconv"
)

// Kind is the storage type of a setting, driving coercion and decode.
type Kind string

const (
	KindBool Kind = "bool"
	KindInt  Kind = "int"
	KindJSON Kind = "json"
)

// Def describes one whitelisted setting. Default is the canonical TEXT form
// stored when no override exists; Public gates whether it is exposed by the
// unauthenticated config endpoint.
type Def struct {
	Key     string
	Kind    Kind
	Default string
	Public  bool
}

// Definitions is the ordered whitelist. A setting absent from this slice can
// never be read or written.
var Definitions = []Def{
	{Key: "chat.player.enabled", Kind: KindBool, Default: "true", Public: true},
	{Key: "chat.spectator.enabled", Kind: KindBool, Default: "true", Public: true},
	{Key: "danmaku.enabled", Kind: KindBool, Default: "true", Public: true},
	{Key: "danmaku.cooldown_ms", Kind: KindInt, Default: "2000", Public: true},
	{Key: "items.enabled", Kind: KindBool, Default: "true", Public: true},
	{Key: "items.cooldown_ms", Kind: KindInt, Default: "30000", Public: true},
	{Key: "items.defs", Kind: KindJSON, Default: `[{"key":"sing","emoji":"🎤","label":"唱歌","image":"assets/art/user-edited/sing.webp"},{"key":"success","emoji":"🎉","label":"大成功","image":"assets/art/user-edited/success.webp"},{"key":"fumble","emoji":"💥","label":"大失败","image":"assets/art/user-edited/fumble.webp"},{"key":"match","emoji":"💞","label":"配了你","image":"assets/art/user-edited/match.webp"}]`, Public: true},
	{Key: "lobby.enabled", Kind: KindBool, Default: "true", Public: true},
	{Key: "friends.enabled", Kind: KindBool, Default: "true", Public: true},
}

// Find returns the definition for key and whether it is whitelisted.
func Find(key string) (Def, bool) {
	for _, def := range Definitions {
		if def.Key == key {
			return def, true
		}
	}
	return Def{}, false
}

// Validate coerces a decoded JSON value into the canonical stored TEXT form for
// the key's kind. It returns ok=false for unknown keys or type mismatches:
// bool requires a JSON boolean, int requires a non-negative whole number, and
// json accepts any value which is re-encoded compactly.
func Validate(key string, rawJSONValue any) (string, bool) {
	def, ok := Find(key)
	if !ok {
		return "", false
	}
	switch def.Kind {
	case KindBool:
		b, ok := rawJSONValue.(bool)
		if !ok {
			return "", false
		}
		if b {
			return "true", true
		}
		return "false", true
	case KindInt:
		f, ok := rawJSONValue.(float64)
		if !ok || math.IsNaN(f) || math.IsInf(f, 0) || f != math.Trunc(f) || f < 0 {
			return "", false
		}
		return strconv.FormatInt(int64(f), 10), true
	case KindJSON:
		encoded, err := json.Marshal(rawJSONValue)
		if err != nil {
			return "", false
		}
		return string(encoded), true
	default:
		return "", false
	}
}

// Decode turns a canonical stored value back into its typed Go value for a
// response body. Unparseable stored values fall back to the zero value.
func Decode(def Def, stored string) any {
	switch def.Kind {
	case KindBool:
		return stored == "true"
	case KindInt:
		n, err := strconv.ParseInt(stored, 10, 64)
		if err != nil {
			return int64(0)
		}
		return n
	case KindJSON:
		var decoded any
		if err := json.Unmarshal([]byte(stored), &decoded); err != nil {
			return nil
		}
		return decoded
	default:
		return stored
	}
}
