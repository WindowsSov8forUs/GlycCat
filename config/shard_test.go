package config

import "testing"

func TestManualShardRequiresValidPair(t *testing.T) {
	for _, tc := range []struct {
		name  string
		id    *uint32
		count uint32
		valid bool
	}{
		{name: "auto", valid: true},
		{name: "missing count", id: shardID(0)},
		{name: "missing id", count: 2},
		{name: "out of range", id: shardID(2), count: 2},
		{name: "manual", id: shardID(1), count: 2, valid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			conf := DefaultConfig()
			conf.Account.AppID = 123
			conf.Account.AppSecret = "test-secret"
			conf.Account.WebHook.Enable = false
			conf.Account.WebSocket.Enable = true
			conf.Account.WebSocket.ShardID = tc.id
			conf.Account.WebSocket.ShardCount = tc.count
			err := conf.NormalizeAndValidate()
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v, err=%v", tc.valid, err)
			}
		})
	}
}

func shardID(value uint32) *uint32 { return &value }
