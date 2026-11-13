package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// The schema is hand-written documentation. Without a drift check it silently
// describes a different format than the decoder accepts, which is worse than no
// documentation at all.
type schemaNode struct {
	Properties map[string]json.RawMessage `json:"properties"`
	Required   []string                   `json:"required"`
	Items      *schemaNode                `json:"items"`
	Ref        string                     `json:"$ref"`
	Defs       map[string]*schemaNode     `json:"$defs"`
}

func loadSchema(t *testing.T) *schemaNode {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "docs", "timeline.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	var node schemaNode
	if err := json.Unmarshal(raw, &node); err != nil {
		t.Fatal(err)
	}
	return &node
}

func jsonFieldNames(t *testing.T, value any) map[string]bool {
	t.Helper()
	kind := reflect.TypeOf(value)
	names := map[string]bool{}
	for i := 0; i < kind.NumField(); i++ {
		field := kind.Field(i)
		name := strings.Split(field.Tag.Get("json"), ",")[0]
		if name == "" || name == "-" {
			t.Fatalf("%s.%s 缺少 json 标签", kind.Name(), field.Name)
		}
		names[name] = true
	}
	return names
}

// requiredInGo mirrors the decoder rule: pointer or omitempty fields may be
// absent, everything else must be present.
func requiredInGo(t *testing.T, value any) map[string]bool {
	t.Helper()
	kind := reflect.TypeOf(value)
	required := map[string]bool{}
	for i := 0; i < kind.NumField(); i++ {
		field := kind.Field(i)
		tag := strings.Split(field.Tag.Get("json"), ",")
		name := tag[0]
		if name == "" || name == "-" {
			continue
		}
		optional := field.Type.Kind() == reflect.Ptr
		for _, flag := range tag[1:] {
			if flag == "omitempty" {
				optional = true
			}
		}
		if !optional {
			required[name] = true
		}
	}
	return required
}

func compareSets(t *testing.T, label string, schemaSet, goSet map[string]bool) {
	t.Helper()
	var missing, extra []string
	for name := range goSet {
		if !schemaSet[name] {
			missing = append(missing, name)
		}
	}
	for name := range schemaSet {
		if !goSet[name] {
			extra = append(extra, name)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)
	if len(missing) > 0 {
		t.Errorf("%s: Go 有但 schema 未声明 %v", label, missing)
	}
	if len(extra) > 0 {
		t.Errorf("%s: schema 有但 Go 未实现 %v", label, extra)
	}
}

func schemaRequired(node *schemaNode) map[string]bool {
	set := map[string]bool{}
	for _, name := range node.Required {
		set[name] = true
	}
	return set
}

func TestSchemaMatchesDecoder(t *testing.T) {
	schema := loadSchema(t)
	clips := schema.Properties["clips"]
	if clips == nil {
		t.Fatal("schema 缺少 clips")
	}
	var clipList schemaNode
	if err := json.Unmarshal(clips, &clipList); err != nil {
		t.Fatal(err)
	}
	clipDef, ok := schema.Defs["Clip"]
	if !ok || !strings.HasSuffix(clipList.Items.Ref, "/Clip") {
		t.Fatal("schema 的 clips.items 必须引用 $defs/Clip")
	}

	for _, tc := range []struct {
		name   string
		schema *schemaNode
		goType any
	}{
		{"Project", schema, Project{}},
		{"Clip", clipDef, Clip{}},
	} {
		compareSets(t, tc.name+" 字段", jsonPropertyNames(t, tc.schema), jsonFieldNames(t, tc.goType))
		compareSets(t, tc.name+" 必填", schemaRequired(tc.schema), requiredInGo(t, tc.goType))
	}

	var trackList schemaNode
	if err := json.Unmarshal(schema.Properties["tracks"], &trackList); err != nil {
		t.Fatal(err)
	}
	compareSets(t, "Track 字段", jsonPropertyNames(t, trackList.Items), jsonFieldNames(t, Track{}))
	compareSets(t, "Track 必填", schemaRequired(trackList.Items), requiredInGo(t, Track{}))
}

func jsonPropertyNames(t *testing.T, node *schemaNode) map[string]bool {
	t.Helper()
	if node == nil {
		t.Fatal("schema 节点缺失")
	}
	names := map[string]bool{}
	for name := range node.Properties {
		names[name] = true
	}
	if len(names) == 0 {
		t.Fatal("schema 节点没有 properties")
	}
	return names
}

// Every shipped example must be describable by the schema, otherwise the docs
// and the sample data disagree.
func TestExamplesRespectSchema(t *testing.T) {
	schema := loadSchema(t)
	var clipList schemaNode
	if err := json.Unmarshal(schema.Properties["clips"], &clipList); err != nil {
		t.Fatal(err)
	}
	clipFields := jsonPropertyNames(t, schema.Defs["Clip"])
	rootFields := jsonPropertyNames(t, schema)
	files, err := filepath.Glob(filepath.Join("..", "..", "examples", "*.json"))
	if err != nil || len(files) == 0 {
		t.Fatalf("找不到样例工程: %v", err)
	}
	for _, file := range files {
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		var doc struct {
			Tracks []map[string]any `json:"tracks"`
			Clips  []map[string]any `json:"clips"`
			Extra  map[string]any   `json:"-"`
		}
		if err := json.Unmarshal(raw, &doc); err != nil {
			t.Fatalf("%s: %v", file, err)
		}
		var root map[string]any
		if err := json.Unmarshal(raw, &root); err != nil {
			t.Fatal(err)
		}
		for name := range root {
			if !rootFields[name] {
				t.Errorf("%s: 顶层字段 %s 不在 schema 中", file, name)
			}
		}
		for _, name := range schema.Required {
			if _, ok := root[name]; !ok {
				t.Errorf("%s: 缺少必填字段 %s", file, name)
			}
		}
		for index, clip := range doc.Clips {
			for name := range clip {
				if !clipFields[name] {
					t.Errorf("%s: 第 %d 个片段字段 %s 不在 schema 中", file, index+1, name)
				}
			}
			for _, name := range schema.Defs["Clip"].Required {
				if _, ok := clip[name]; !ok {
					t.Errorf("%s: 第 %d 个片段缺少必填字段 %s", file, index+1, name)
				}
			}
		}
		if _, err := DecodeProject(raw); err != nil {
			t.Errorf("%s: 解码失败 %v", file, err)
		}
	}
}
