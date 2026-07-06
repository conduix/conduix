package output

import (
	"reflect"
	"testing"

	"github.com/conduix/conduix/pipeline-core/pkg/source"
)

func delRec() source.Record {
	return source.Record{Data: map[string]any{
		"_cdc_type":            "delete",
		"_primary_key_columns": []string{"id"},
		"_primary_key":         []any{7},
		"_old_data":            map[string]any{"id": 7, "name": "x"},
	}}
}
func insRec(id int) source.Record {
	return source.Record{Data: map[string]any{"_cdc_type": "insert", "id": id}}
}

// cdcDelete=true 면 delete 이벤트가 upsert/delete 로 분리된다.
func TestPartitionCDC_SplitsDeletes(t *testing.T) {
	o := &SQLOutput{cdcDelete: true}
	up, del := o.partitionCDC([]source.Record{insRec(1), delRec(), insRec(2)})
	if len(up) != 2 || len(del) != 1 {
		t.Fatalf("split: upsert=%d delete=%d, want 2/1", len(up), len(del))
	}
}

// cdcDelete=false 면 delete 도 upsert 로 취급(삭제 반영 안 함).
func TestPartitionCDC_Disabled(t *testing.T) {
	o := &SQLOutput{cdcDelete: false}
	up, del := o.partitionCDC([]source.Record{insRec(1), delRec()})
	if len(up) != 2 || len(del) != 0 {
		t.Fatalf("disabled: upsert=%d delete=%d, want 2/0", len(up), len(del))
	}
}

// deleteKey 는 _primary_key_columns + _primary_key(위치대응)로 (컬럼,값)을 뽑는다.
func TestDeleteKey_FromPrimaryKey(t *testing.T) {
	o := &SQLOutput{}
	cols, vals := o.deleteKey(delRec())
	if !reflect.DeepEqual(cols, []string{"id"}) {
		t.Errorf("cols=%v, want [id]", cols)
	}
	if len(vals) != 1 || vals[0] != 7 {
		t.Errorf("vals=%v, want [7]", vals)
	}
}

// PK 정보가 없으면 conflict_columns 로 폴백하고 값은 _old_data 에서 찾는다.
func TestDeleteKey_FallbackToConflictColumns(t *testing.T) {
	o := &SQLOutput{conflictColumns: []string{"id"}}
	r := source.Record{Data: map[string]any{
		"_cdc_type": "delete",
		"_old_data": map[string]any{"id": 42, "name": "y"},
	}}
	cols, vals := o.deleteKey(r)
	if !reflect.DeepEqual(cols, []string{"id"}) || len(vals) != 1 || vals[0] != 42 {
		t.Errorf("fallback cols=%v vals=%v, want [id]/[42]", cols, vals)
	}
}
