package core

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

// seedTasks writes n finished images from two days ago in one transaction, as a
// data folder that has been used for months would hold.
func seedTasks(tb testing.TB, s *Service, n int, trial bool) {
	tb.Helper()
	tx, e := s.store.db.Begin()
	if e != nil {
		tb.Fatal(e)
	}
	old := time.Now().Add(-48 * time.Hour)
	c := Connection{ID: "seed-conn", Name: "种子线路", Kind: "image", Model: "m"}
	for i := 0; i < n; i++ {
		at := old.Add(time.Duration(i) * time.Millisecond).UTC().Format(time.RFC3339Nano)
		t := Task{ID: fmt.Sprintf("seed-%06d", i), BatchID: fmt.Sprintf("batch-%d", i/12), CreatedAt: at, UpdatedAt: at, Status: "succeeded", Prompt: "一张历史图片的提示词，长度和真实提示词差不多。" + fmt.Sprint(i), Model: "m", Size: "1024x1024", Connection: c, AssetIDs: []string{"a"}, References: []string{}, Trial: trial && i < 20,
			Diagnostic: GenerationDiagnostic{Stage: "已保存", StartedAt: at, ElapsedMS: 40000, HTTPStatus: 200}}
		b, _ := json.Marshal(t)
		if _, e = tx.Exec(`INSERT INTO objects(kind,id,body,created) VALUES('task',?,?,?)`, t.ID, string(b), at); e != nil {
			tb.Fatal(e)
		}
	}
	if e = tx.Commit(); e != nil {
		tb.Fatal(e)
	}
}

func benchService(b *testing.B, n int) *Service {
	s := testService(b)
	seedTasks(b, s, n, false)
	return s
}

func benchmarkScheduling(b *testing.B, n int) {
	s := benchService(b, n)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.claimTask(false) // nothing queued: the cost of one idle scheduling pass
		s.claimTask(true)
	}
}

func benchmarkStatus(b *testing.B, n int) {
	s := benchService(b, n)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.LicenseStatus()
		if _, e := s.Dashboard(); e != nil {
			b.Fatal(e)
		}
		if _, e := s.ListTasks("", 0, 50); e != nil {
			b.Fatal(e)
		}
	}
}

func BenchmarkScheduling5k(b *testing.B)  { benchmarkScheduling(b, 5_000) }
func BenchmarkScheduling50k(b *testing.B) { benchmarkScheduling(b, 50_000) }
func BenchmarkStatus5k(b *testing.B)      { benchmarkStatus(b, 5_000) }
func BenchmarkStatus50k(b *testing.B)     { benchmarkStatus(b, 50_000) }
