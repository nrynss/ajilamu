package assemble

import "fmt"

// Duck compressor settings key the bed on the speech layer.
// threshold 0.016 sits near -36 dB. Digital silence stays under it.
// Voiced speech crosses it.
// ratio 12 pulls the bed down once speech crosses the threshold.
// attack 5 ms starts the dip inside the first syllable.
// release 80 ms restores unity gain inside a one-second silent gap.
// knee 1 is a hard knee so a silent sidechain never reduces the bed.
// detection peak follows speech onsets.
// makeup 1 leaves unducked intervals at the original bed level.
// mix 1 uses the fully compressed wet signal.
// sidechaincompress can drop the tail of its output.
// A one second pad keeps that tail inside the compressor window.
// The first amix input is the original bed so duration=first matches the bed.
const (
	duckThreshold  = 0.016
	duckRatio      = 12.0
	duckAttackMS   = 5.0
	duckReleaseMS  = 80.0
	duckKnee       = 1.0
	duckMakeup     = 1.0
	duckMix        = 1.0
	duckPadSeconds = 1.0
)

func duckFilter() string {
	return fmt.Sprintf(
		"[0:a]asplit=2[orig][to_duck];"+
			"[to_duck]apad=pad_dur=%g[bedpad];"+
			"[1:a]apad=pad_dur=%g[scpad];"+
			"[bedpad][scpad]sidechaincompress=threshold=%g:ratio=%g:attack=%g:release=%g:knee=%g:detection=peak:makeup=%g:mix=%g[ducked];"+
			"[orig][ducked]amix=inputs=2:duration=first:weights=0 1:normalize=0:dropout_transition=0[duckedfull];"+
			"[duckedfull][1:a]amix=inputs=2:duration=first:normalize=0:dropout_transition=0[out]",
		duckPadSeconds, duckPadSeconds,
		duckThreshold, duckRatio, duckAttackMS, duckReleaseMS, duckKnee, duckMakeup, duckMix)
}

// Duck mixes speech over the bed and dips the bed under voiced speech.
// Creator-supplied music skips the compressor and overlays at unity.
func (b Bed) Duck(speech, output string) error {
	if err := validateBedFormat(b.Format); err != nil {
		return err
	}
	if b.File == "" || speech == "" {
		return fmt.Errorf("duck needs a bed and a speech layer")
	}
	if b.SeparateMusic {
		return b.Overlay(speech, output)
	}
	if err := distinctOutput(output, b.File, speech); err != nil {
		return err
	}
	return renderMix([]string{b.File, speech}, output, b.Format, duckFilter())
}
