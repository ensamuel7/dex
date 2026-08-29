package stdlib

import (
	_ "embed"
	"os/exec"
	"strings"

	"github.com/ensamuel7/dex/ast"
)

//go:embed cruntime/kafka.c
var kafkaRuntime string

// librdkafka is found the same way the database drivers are: ask pkg-config,
// and if it is not there, build a module whose every call says so rather than
// failing the build of a program that may never call it.
func detectKafkaFlags() []string {
	out, err := exec.Command("pkg-config", "--cflags", "rdkafka").Output()
	if err != nil {
		return nil
	}
	flags := []string{"-DDEX_HAS_KAFKA"}
	flags = append(flags, strings.Fields(strings.TrimSpace(string(out)))...)
	if libs, err := exec.Command("pkg-config", "--libs", "rdkafka").Output(); err == nil {
		flags = append(flags, strings.Fields(strings.TrimSpace(string(libs)))...)
	} else {
		flags = append(flags, "-lrdkafka")
	}
	return flags
}

func init() {
	Register(&Module{
		Name: "kafka",
		Funcs: map[string]FuncDef{
			"available": {
				Params:     nil,
				ParamNames: nil,
				ReturnType: ast.TypeBool,
				CName:      "dex_kafka_available",
				Doc:        "True when this build found librdkafka.",
			},

			"producer": {
				Params:     []ast.Type{ast.TypeString},
				ParamNames: []string{"brokers"},
				ReturnType: ast.TypeInt,
				CName:      "dex_kafka_producer",
				Doc:        "Open a producer against a comma-separated broker list. Returns a handle, or -1.",
			},
			"produce": {
				Params:     []ast.Type{ast.TypeInt, ast.TypeString, ast.TypeString, ast.TypeString},
				ParamNames: []string{"producer", "topic", "key", "value"},
				ReturnType: ast.TypeBool,
				CName:      "dex_kafka_produce",
				Doc:        "Queue a message. The key picks the partition, so one key keeps its order.",
			},
			"ready": {
				Params:     []ast.Type{ast.TypeInt, ast.TypeInt},
				ParamNames: []string{"handle", "timeoutMs"},
				ReturnType: ast.TypeBool,
				CName:      "dex_kafka_ready",
				Doc:        "Ask the cluster for metadata: true when a broker actually answered.",
			},
			"flush": {
				Params:     []ast.Type{ast.TypeInt, ast.TypeInt},
				ParamNames: []string{"producer", "timeoutMs"},
				ReturnType: ast.TypeInt,
				CName:      "dex_kafka_flush",
				Doc:        "Wait for queued messages to be delivered. Returns how many are still waiting.",
			},

			"consumer": {
				Params:     []ast.Type{ast.TypeString, ast.TypeString},
				ParamNames: []string{"brokers", "groupId"},
				ReturnType: ast.TypeInt,
				CName:      "dex_kafka_consumer",
				Doc:        "Open a consumer in a group, reading from the earliest offset. Returns a handle, or -1.",
			},
			"subscribe": {
				Params:     []ast.Type{ast.TypeInt, ast.TypeString},
				ParamNames: []string{"consumer", "topic"},
				ReturnType: ast.TypeBool,
				CName:      "dex_kafka_subscribe",
				Doc:        "Add a topic to this consumer's subscription.",
			},
			"poll": {
				Params:     []ast.Type{ast.TypeInt, ast.TypeInt},
				ParamNames: []string{"consumer", "timeoutMs"},
				ReturnType: ast.TypeString,
				CName:      "dex_kafka_poll",
				Doc:        "Wait up to timeoutMs for one message and return its value. Empty means none arrived.",
			},
			"lastTopic": {
				Params:     []ast.Type{ast.TypeInt},
				ParamNames: []string{"consumer"},
				ReturnType: ast.TypeString,
				CName:      "dex_kafka_last_topic",
				Doc:        "The topic the last polled message came from.",
			},
			"lastKey": {
				Params:     []ast.Type{ast.TypeInt},
				ParamNames: []string{"consumer"},
				ReturnType: ast.TypeString,
				CName:      "dex_kafka_last_key",
				Doc:        "The key of the last polled message.",
			},
			"commit": {
				Params:     []ast.Type{ast.TypeInt},
				ParamNames: []string{"consumer"},
				ReturnType: ast.TypeBool,
				CName:      "dex_kafka_commit",
				Doc:        "Commit the offsets of what has been polled. Auto-commit is off, so a crash replays.",
			},

			"close": {
				Params:     []ast.Type{ast.TypeInt},
				ParamNames: []string{"handle"},
				ReturnType: ast.TypeVoid,
				CName:      "dex_kafka_close",
				Doc:        "Flush or leave the group, then close. Safe on either kind of handle.",
			},
		},
		CRuntime: kafkaRuntime,
		CFlags:   append([]string{"-pthread"}, detectKafkaFlags()...),
	})
}
