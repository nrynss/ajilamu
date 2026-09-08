package ledger

import (
	"encoding/json"
	"reflect"
	"strconv"
	"testing"

	"github.com/nrynss/ajilamu/internal/api"
)

func TestApplyTakePeaksBoundariesAndAPIWire(t *testing.T) {
	for _, size := range []int{64, 128} {
		t.Run("accepts-size-"+strconv.Itoa(size), func(t *testing.T) {
			peaks := waveformPeaks(size)
			take := &api.Take{File: "/does/not/exist/take.wav"}
			if err := ApplyTakePeaks(take, peaks); err != nil {
				t.Fatalf("ApplyTakePeaks(%d values): %v", size, err)
			}
			want := make([]int, len(peaks))
			for index, peak := range peaks {
				want[index] = int(peak)
			}
			if !reflect.DeepEqual(take.Peaks, want) {
				t.Errorf("mapped peaks = %v, want %v", take.Peaks, want)
			}

			peaks[0] = 0
			if take.Peaks[0] != want[0] {
				t.Errorf("mapped peaks changed after input mutation: got %d, want %d", take.Peaks[0], want[0])
			}

			body, err := json.Marshal(take)
			if err != nil {
				t.Fatalf("marshal API take: %v", err)
			}
			var wire map[string]any
			if err := json.Unmarshal(body, &wire); err != nil {
				t.Fatalf("decode API take JSON: %v", err)
			}
			values, ok := wire["peaks"].([]any)
			if !ok || len(values) != size {
				t.Fatalf("wire peaks = %T %v, want %d numeric values", wire["peaks"], wire["peaks"], size)
			}
			for index, value := range values {
				if value != float64(want[index]) {
					t.Errorf("wire peak %d = %v, want numeric %d", index, value, want[index])
				}
			}
		})
	}
}

func TestApplyTakePeaksRejectsInvalidLengthsAndClearsEmpty(t *testing.T) {
	for _, size := range []int{63, 129} {
		t.Run("rejects-size-"+strconv.Itoa(size), func(t *testing.T) {
			take := &api.Take{Peaks: []int{7}}
			if err := ApplyTakePeaks(take, waveformPeaks(size)); err == nil {
				t.Fatalf("ApplyTakePeaks accepted %d values", size)
			}
			if !reflect.DeepEqual(take.Peaks, []int{7}) {
				t.Errorf("invalid input changed existing peaks to %v", take.Peaks)
			}
		})
	}

	take := &api.Take{Peaks: []int{7}}
	if err := ApplyTakePeaks(take, nil); err != nil {
		t.Fatalf("ApplyTakePeaks(empty): %v", err)
	}
	if take.Peaks != nil {
		t.Errorf("empty peaks left stale values: %v", take.Peaks)
	}
	body, err := json.Marshal(take)
	if err != nil {
		t.Fatalf("marshal empty API take: %v", err)
	}
	var wire map[string]any
	if err := json.Unmarshal(body, &wire); err != nil {
		t.Fatalf("decode empty API take JSON: %v", err)
	}
	if _, ok := wire["peaks"]; ok {
		t.Errorf("empty peaks remained in API JSON: %s", body)
	}
}

func waveformPeaks(size int) []uint8 {
	peaks := make([]uint8, size)
	for index := range peaks {
		peaks[index] = uint8(index)
	}
	return peaks
}
