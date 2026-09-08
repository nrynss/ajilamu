package fit

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"time"

	"github.com/nrynss/ajilamu/internal/media"
	"github.com/nrynss/ajilamu/internal/types"
)

// One ffmpeg atempo filter accepts a ratio inside this range.
// A correction beyond it needs a chain of filters, which this package does not build.
const (
	// MinAtempoRatio is the slowest single atempo filter ffmpeg accepts.
	MinAtempoRatio = 0.5
	// MaxAtempoRatio is the fastest single atempo filter ffmpeg accepts.
	MaxAtempoRatio = 2.0
)

// DefaultMaxStretchLong caps the overrun that atempo may repair, by default.
// An overrun beyond this line belongs to the shorter rewrite path.
//
// Speeding speech up hides its artefacts better than slowing it down, so the
// long side keeps the wider budget of the two.
const DefaultMaxStretchLong = 0.08

// DefaultMaxStretchShort caps the underrun that atempo may repair, by default.
// An underrun beyond this line belongs to the fuller rewrite path.
//
// The short side carries the tighter budget because slowing speech sounds more
// artificial than speeding it by the same amount.
//
// This 5 percent comes from general speech time tailoring practice. It is
// UNVERIFIED BY EAR. No measurement in this repository decides it, and no
// listener has ruled on it yet. The listening ladder is the instrument that
// will settle it. Its measured table and its regeneration command live in the
// T2.5 handoff entry in dev-diary/PHASE-2-fit-loop.md.
//
// Where the number is a guess, the error falls toward a Gemini rewrite, which
// sounds right. It does not fall toward a stretched take that sounds wrong and
// logs as repaired.
const DefaultMaxStretchShort = 0.05

// MaxStretchLimit caps any budget a caller may resolve, on either side.
//
// The budget is an input, so without a ceiling a caller could resolve 0.45 and
// hand segment 8 back to atempo. That reopens the exact defect this project
// exists to close. A 40.9 percent underrun is a rewrite, whatever a
// configuration file says.
//
// This 0.10 is REASONED, NOT HEARD. It carries the same honesty label as
// DefaultMaxStretchShort above. No listener has ruled on it, and no
// measurement in this repository decides it.
//
// The listening ladder in the T2.5 handoff entry in
// dev-diary/PHASE-2-fit-loop.md reaches ratio 0.9000 at its lowest rung, a 10
// percent correction. The ceiling stops there because the project chose to
// respect the ladder's reach. Nobody has judged that audio.
//
// Rendering more rungs does not widen this ceiling. A render produces a file
// to judge and settles nothing on its own. Only a listener ruling widens the
// ceiling, and the ruling belongs in the handoff entry beside the ladder.
//
// The ceiling binds one call. It says nothing about a take's history, because
// this package holds no provenance and cannot tell a fresh synthesis from its
// own earlier output. See StretchWithLimits for what a chain of legal calls
// reaches, and for whose job the running total is.
const MaxStretchLimit = 0.10

// This package does not read types.FitThreshold, and that divergence is
// deliberate. The two constants answer different questions.
//
// types.Fit.Fits() asks whether a take is close enough to ship as it stands.
// The stretch limits ask whether a take is close enough to repair locally with
// atempo instead of paying for a rewrite. A take can report Fits true at 6
// percent short and still plan a rewrite here. Six percent of slowing is
// audible even though 6 percent of drift is shippable.
//
// types.FitThreshold is frozen P1 and other packages read it. Changing it here
// is out of scope for T2.5. P1 and T2.6 reconcile the two deliberately.

// StretchDeadBand is the miss that a stretch cannot usefully improve on.
// A take already this close stays untouched, and a stretched take must land inside it.
//
// Two independent absolute quantities set the value. ffmpeg atempo lands a few
// tens of milliseconds away from the arithmetic target, because it quantises to
// its own analysis windows. Measured over the eight fixture takes at their
// corrective ratios, that error reached 25 ms and stayed under 20 ms inside the
// repair band. Broadcast practice also tolerates roughly 45 ms of audio lead
// before a viewer reads a lip as out of sync. 40 ms clears the measured error
// and stays under the perceptual line.
//
// Move the band by editing this one constant.
const StretchDeadBand = 40 * time.Millisecond

// Errors a caller can branch on. Most name a decision this package made, not
// a failure of ffmpeg. ErrSourceUnreadable and ErrRenderFailed name a failure
// ffmpeg or ffprobe actually reported, wrapped so a caller can still classify
// it instead of matching on raw tool text.
//
// CAUTION on the wrapped text. It is raw tool output and it carries no bound.
// One ErrRenderFailed from an extensionless output path measured 2451 bytes
// across 18 lines on ffmpeg n9.0.1, and it opens with the full build
// configuration banner. ffmpeg puts the actual reason last, so the opening
// lines say nothing a reader wants.
//
// Never put Error() into a viewer facing string or a database column. Two
// sinks downstream invite exactly that. The doc on api.ProgressEvent.Sentence
// calls it a complete natural-language update, and ledger.takeRow.RepairDetail
// holds a plain string column. Branch on the sentinel and write your own
// sentence from it. Log the wrapped text where an operator reads it.
//
// This package wraps the message rather than trimming it, because the reason
// sits after the banner and a blind trim would drop the reason. Bounding the
// tool output belongs to internal/media and T0.3, which build the string.
var (
	// ErrRatioRange reports a requested ratio outside one atempo filter's range.
	ErrRatioRange = errors.New("atempo ratio outside the supported range")
	// ErrDeadBand reports a take already close enough that stretching costs more than it gains.
	ErrDeadBand = errors.New("take already lies inside the stretch dead band")
	// ErrOverrunTooLarge reports an overrun that only a shorter rewrite can fix.
	ErrOverrunTooLarge = errors.New("overrun exceeds the atempo repair band")
	// ErrUnderrunTooLarge reports an underrun that only a fuller rewrite can fix.
	ErrUnderrunTooLarge = errors.New("underrun exceeds the atempo repair band")
	// ErrLandedOutside reports a stretch that ran but missed the slot.
	// Stretch removes the output it wrote before it returns this error.
	ErrLandedOutside = errors.New("stretched take landed outside its slot")
	// ErrRenderFailed reports an ffmpeg or ffprobe failure once rendering has
	// started, such as an output path with no extension. It also reports a
	// claim failure before rendering starts, such as a missing output directory.
	// The underlying message wraps inside it.
	ErrRenderFailed = errors.New("atempo render failed")
	// ErrNoSlot reports a slot duration that is zero or negative.
	ErrNoSlot = errors.New("slot duration must be positive")
	// ErrOutputExists reports an output path that already holds a take.
	ErrOutputExists = errors.New("output take already exists")
	// ErrPathRequired reports an empty source path or an empty output path.
	ErrPathRequired = errors.New("take path is required")
	// ErrSameFile reports an output that names its own source take.
	ErrSameFile = errors.New("output take must differ from its source")
	// ErrSourceMissing reports a source take that is not on disk.
	ErrSourceMissing = errors.New("source take does not exist")
	// ErrSourceUnreadable reports a source take that exists but that ffprobe
	// could not read, such as a directory given as a source or a file it
	// cannot decode. The underlying message wraps inside it.
	ErrSourceUnreadable = errors.New("source take could not be probed")
	// ErrLimits reports a stretch budget this package will not apply.
	ErrLimits = errors.New("stretch limits must sit above 0 and at or below MaxStretchLimit")
)

// StretchLimits carries the repair budget for one take, one side at a time.
//
// The caller resolves these values and passes them in. This package never
// resolves them, so policy does not leak into the mechanism. The fit loop
// layers project default, then language, then content slice, narrowest wins.
// T2.6 or T2.7 owns that resolution. This file only receives the answer.
//
// A tolerance varies by language and by content. Malayalam, German and Spanish
// do not share a syllable rate, and a fast passage tolerates less than a slow
// one.
type StretchLimits struct {
	// Short caps the underrun that atempo may repair, as a fraction of the slot.
	Short float64
	// Long caps the overrun that atempo may repair, as a fraction of the slot.
	Long float64
}

// DefaultStretchLimits returns the project default budget for both sides.
func DefaultStretchLimits() StretchLimits {
	return StretchLimits{Short: DefaultMaxStretchShort, Long: DefaultMaxStretchLong}
}

// Valid reports whether both sides carry a budget this package can apply.
// A usable budget sits above 0 and at or below MaxStretchLimit. That range
// rejects NaN, an infinity, a negative fraction, and a budget so wide that it
// would hand a rewrite back to atempo.
func (l StretchLimits) Valid() bool {
	return usableLimit(l.Short) && usableLimit(l.Long)
}

func usableLimit(v float64) bool {
	// A NaN comparison is false, so this test rejects NaN as well.
	return v > 0 && v <= MaxStretchLimit
}

// For returns the budget that governs one signed delta.
// A take that runs long reads Long. A take that runs short or exact reads Short.
// Neither side reads the other side's value.
func (l StretchLimits) For(delta time.Duration) float64 {
	if delta > 0 {
		return l.Long
	}
	return l.Short
}

// StretchPlan states the repair one measured fit needs.
type StretchPlan struct {
	// Repair names the strategy. RepairAtempo is the only one this package applies.
	Repair types.Repair
	// Ratio holds Measured divided by Slot. It carries meaning only for RepairAtempo.
	Ratio float64
	// Band records the dead band applied to this slot on this take's own side.
	Band time.Duration
	// Limit records the budget this plan applied, as a fraction of the slot.
	Limit float64
}

// StretchResult records one applied atempo stretch.
type StretchResult struct {
	// In and Out name the source take and the stretched take.
	In  string
	Out string
	// Ratio is the requested atempo ratio. media.Atempo renders it to four decimals.
	Ratio float64
	// Before measures the source take against the slot.
	Before types.Fit
	// After measures the stretched take against the same slot.
	After types.Fit
	// Band records the dead band the landing check used.
	Band time.Duration
	// Limits records the budget this call applied.
	Limits StretchLimits
}

// deadBand returns the tolerance that applies to one slot on one side.
//
// The band never exceeds that side's own repair budget, so a very short slot
// cannot hide a real misfit. A take that runs long reads the long budget and a
// take that runs short reads the short budget. Editing one side leaves the
// other side's band exactly where it was.
func deadBand(slot time.Duration, delta time.Duration, lim StretchLimits) time.Duration {
	if slot <= 0 || !lim.Valid() {
		return 0
	}
	band := StretchDeadBand
	if ceiling := time.Duration(float64(slot) * lim.For(delta)); ceiling < band {
		band = ceiling
	}
	return band
}

func absDuration(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}

// delta returns the signed miss of one measured fit, derived and never read.
//
// This package derives rather than trusts. types.Fit exports Slot, Measured
// and Delta, and Delta names a value the other two already decide. A composite
// literal may omit it, and Go then fills it with zero. A literal may also
// contradict it, because nothing in the type checks the two against each
// other.
//
// Trusting the field puts the 2026-09-07 defect one call away.
// PlanStretch(types.Fit{Slot: 7110ms, Measured: 4200ms}) reported no repair
// for a take running 40.9 percent short, and a stale Delta of -100ms sent that
// same take to atempo at ratio 0.590717. Deriving refuses both.
//
// Two sibling packages already re-derive this invariant instead of trusting
// it. internal/ledger/takes.go refuses a row whose delta is not measured minus
// slot, and internal/fixtures/load.go checks the same thing. Whether the check
// belongs on types.Fit itself is a P1 contract question, recorded in
// dev-diary/adversarial-review/t2.5-remediation-round3.md.
//
// Nothing in this package reads f.Delta for a routing or a band decision.
func delta(f types.Fit) time.Duration {
	return f.Measured - f.Slot
}

// PlanStretch chooses the repair for one measured fit under the default budget.
// Call PlanStretchWithLimits to pass a budget the fit loop resolved itself.
func PlanStretch(ctx context.Context, f types.Fit) StretchPlan {
	return PlanStretchWithLimits(ctx, f, DefaultStretchLimits())
}

// PlanStretchWithLimits chooses the repair for one measured fit under lim.
//
// It returns RepairNone when the take already sits inside the dead band.
// It returns RepairAtempo when the miss is real and lies inside the repair band.
// It returns RepairRewrite when the miss exceeds the repair band in either direction.
// It returns RepairManual when the slot carries no positive duration, when
// Measured is negative, and when lim carries a budget this package cannot
// apply. All are operator problems.
//
// The band is direction aware. lim.Long governs an overrun and lim.Short
// governs an underrun. The default short budget sits below types.FitThreshold,
// so a take can report Fits true and still plan a rewrite. That is intended. A
// listener rules on audibility, and the domain threshold does not.
//
// It derives the miss from f.Measured minus f.Slot and ignores f.Delta
// entirely. A caller may hand this function a literal whose Delta is absent or
// stale, and the routing still follows the two durations. See delta for why.
func PlanStretchWithLimits(ctx context.Context, f types.Fit, lim StretchLimits) StretchPlan {
	if f.Slot <= 0 || f.Measured < 0 || !lim.Valid() {
		return StretchPlan{Repair: types.RepairManual}
	}

	miss := delta(f)
	limit := lim.For(miss)
	band := deadBand(f.Slot, miss, lim)
	plan := StretchPlan{Band: band, Limit: limit}

	if absDuration(miss) <= band {
		plan.Repair = types.RepairNone
		return plan
	}

	relative := float64(absDuration(miss)) / float64(f.Slot)
	if relative > limit {
		plan.Repair = types.RepairRewrite
		return plan
	}

	plan.Repair = types.RepairAtempo
	plan.Ratio = f.Ratio()
	return plan
}

// Stretch repairs one take by time stretching it into its slot, under the
// default budget. Call StretchWithLimits to pass a budget the fit loop
// resolved itself.
func Stretch(ctx context.Context, in, out string, slot time.Duration) (StretchResult, error) {
	return StretchWithLimits(ctx, in, out, slot, DefaultStretchLimits())
}

// StretchWithLimits repairs one take by time stretching it into its slot.
//
// It measures the source take, plans the repair, applies atempo, and measures
// the output again. The ratio is Measured divided by Slot. A ratio above 1.0
// speeds up an overrun and a ratio below 1.0 slows down an underrun.
//
// It refuses any take that PlanStretchWithLimits does not send to atempo. It
// returns ErrDeadBand, ErrOverrunTooLarge or ErrUnderrunTooLarge in that case,
// and the result still carries Before and Band so the caller can route the take
// onward. Segment 8 of the fixture set takes that path. Its ratio of 0.59 sits
// inside the atempo range, so only the repair band refuses it.
//
// It returns ErrLandedOutside when atempo ran and the output missed the band.
// ffmpeg reports success on that run, so only the ffprobe re-measure sees it.
//
// Every error leaves the output path empty. A refusal never starts ffmpeg, and
// a stretch that ran and missed gets its output removed before this function
// returns. No failed repair leaves a file that a later run could read as a good
// take. A removal that itself fails joins the returned message, so the caller
// learns that a file survived.
//
// A nil error means a file exists at Out. This call claims that path
// exclusively before ffmpeg starts, so two calls racing for one take name give
// one winner and one ErrOutputExists. The loser writes nothing and removes
// nothing. The guarantee holds against every caller that goes through this
// package. A program that writes or deletes the output path behind this
// package's back can still break it, and no check here can prevent that.
//
// The repair band binds this call, never the take's history. This package
// holds no provenance and cannot tell a fresh synthesis from its own earlier
// output. Six legal calls under a 0.10 budget walked seg_8_try1.wav from
// 4200 ms to 7101 ms against its real 7110 ms slot. Every step returned nil
// and landed inside its own supplied slot, and the finished artifact replans
// as needing no repair. The total correction is 40.9 percent, which this
// package refuses in one call and cannot refuse across six.
//
// So the caller owns the running total. T2.6 or T2.7 must cap total correction
// across attempts, and must refuse a source that is already a stretched
// output. Passing a fresh slot for each attempt is not a guard.
//
// It also returns ErrPathRequired, ErrSameFile, ErrSourceMissing,
// ErrOutputExists, ErrNoSlot and ErrLimits. Every one of those arrives before
// ffmpeg or ffprobe runs.
//
// ErrSourceUnreadable reports a source that exists but that ffprobe could not
// read, such as a directory given as a source or a file it cannot decode.
// ErrRenderFailed reports an ffmpeg or ffprobe failure once rendering has
// started, such as an output path with no extension. It also reports a failure
// to create the output file at all, such as a missing output directory. Both
// wrap the underlying ffmpeg or ffprobe message.
//
// That wrapped message is raw tool output and it carries no bound. One
// ErrRenderFailed from an extensionless output path measured 2451 bytes across
// 18 lines on ffmpeg n9.0.1, and it opens with a build banner. Never route
// Error() into a viewer sentence or a database column. The error block above
// names the two sinks that invite it.
//
// These sentinels cover every failure path this package has walked, not
// every failure path that exists. An unclassified operating system error,
// such as a permission failure, still returns a plain error with no
// sentinel. A caller branching on these should keep a default arm.
//
// ErrRatioRange guards one atempo filter's own range. MaxStretchLimit confines
// every policy derived ratio to roughly 0.90 through 1.10, so this exported
// path cannot reach ErrRatioRange today. The guard stays as defence in depth,
// not as a branch this function's callers should expect to take.
func StretchWithLimits(ctx context.Context, in, out string, slot time.Duration, lim StretchLimits) (StretchResult, error) {
	if err := checkPaths(in, out); err != nil {
		return StretchResult{}, err
	}
	if slot <= 0 {
		return StretchResult{}, fmt.Errorf("%w: got %v", ErrNoSlot, slot)
	}
	if !lim.Valid() {
		return StretchResult{}, fmt.Errorf("%w: got short %g and long %g", ErrLimits, lim.Short, lim.Long)
	}

	before, err := Measure(ctx, in, slot)
	if err != nil {
		return StretchResult{}, fmt.Errorf("%w: %s: %w", ErrSourceUnreadable, in, err)
	}

	plan := PlanStretchWithLimits(ctx, before, lim)
	res := StretchResult{In: in, Out: out, Before: before, Band: plan.Band, Limits: lim}
	miss := delta(before)

	switch plan.Repair {
	case types.RepairAtempo:
		res.Ratio = plan.Ratio
	case types.RepairNone:
		return res, fmt.Errorf("%w: delta %v within %v", ErrDeadBand, miss, plan.Band)
	case types.RepairRewrite:
		if miss > 0 {
			return res, fmt.Errorf("%w: delta %v exceeds %.1f%% of slot %v",
				ErrOverrunTooLarge, miss, lim.Long*100, slot)
		}
		return res, fmt.Errorf("%w: delta %v exceeds %.1f%% of slot %v",
			ErrUnderrunTooLarge, miss, lim.Short*100, slot)
	default:
		return res, fmt.Errorf("no atempo repair for %s on slot %v", plan.Repair, slot)
	}

	return applyStretch(ctx, res)
}

// renderStretch runs atempo and measures the output with ffprobe.
// It trusts no value that ffmpeg reported about its own work.
// It applies no landing check and it leaves the output on disk.
//
// Like applyStretch, this function is unexported. Go package scope still allows
// sibling files in package fit to call it. Sibling files like rewrite.go or
// loop.go must never call this function directly.
func renderStretch(ctx context.Context, res StretchResult) (StretchResult, error) {
	if err := checkRatio(res.Ratio); err != nil {
		return res, err
	}
	if err := media.Atempo(ctx, res.In, res.Out, res.Ratio); err != nil {
		return res, fmt.Errorf("%w: %w", ErrRenderFailed, err)
	}

	after, err := Measure(ctx, res.Out, res.Before.Slot)
	if err != nil {
		return res, fmt.Errorf("%w: %s: %w", ErrRenderFailed, res.Out, err)
	}
	res.After = after
	return res, nil
}

// applyStretch claims the output path, renders one repair, and enforces the
// landing check. It removes the output whenever the stretch failed, so no
// failed repair leaves a take behind.
//
// Go package scope cannot prevent sibling files in package fit from calling
// this function or renderStretch directly. The only defense against sibling
// abuse is the written rule. Sibling files must route all repairs through
// Stretch or StretchWithLimits.
//
// The claim comes first and it is what makes the removal safe. This call
// created the file at res.Out, and no other call through this package holds
// that path at the same time. So the cleanup can only delete its own work. A
// stat cannot promise that, because another call can create the file between
// the stat and the write.
func applyStretch(ctx context.Context, res StretchResult) (StretchResult, error) {
	if err := claimOutput(res.Out); err != nil {
		return res, err
	}

	res, err := renderStretch(ctx, res)
	if err != nil {
		return res, discardOutput(res.Out, err)
	}

	if absDuration(delta(res.After)) > res.Band {
		missed := fmt.Errorf("%w: ratio %.4f gave %v against slot %v, delta %v beyond %v",
			ErrLandedOutside, res.Ratio, res.After.Measured, res.After.Slot, delta(res.After), res.Band)
		return res, discardOutput(res.Out, missed)
	}
	return res, nil
}

// claimOutput takes the output path for this call alone, before ffmpeg runs.
//
// It creates the file exclusively, so the operating system decides the winner
// of a race rather than a stat this package ran earlier. A second call for the
// same path gets ErrOutputExists and writes nothing. That gives the caller one
// rule. A nil error from Stretch means a file exists at Out.
//
// checkPaths already reported an occupied path with a better message, and it
// still runs first. It reports what was true when it looked. This function
// makes the guarantee.
//
// ffmpeg overwrites the empty claim, because media.Atempo passes -y. A claim
// that fails for any other reason, such as a missing output directory, reports
// ErrRenderFailed. Writing the output failed, which is what that sentinel
// names, and the caller sees the same sentinel it saw before this claim
// existed.
func claimOutput(out string) error {
	f, err := os.OpenFile(out, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return fmt.Errorf("%w: %s", ErrOutputExists, out)
		}
		return fmt.Errorf("%w: claim %s: %w", ErrRenderFailed, out, err)
	}
	if err := f.Close(); err != nil {
		return discardOutput(out, fmt.Errorf("%w: claim %s: %w", ErrRenderFailed, out, err))
	}
	return nil
}

// discardOutput removes the file a failed stretch wrote and returns the error
// the caller should see. A removal that itself fails joins the message, which
// tells the caller that a wrong length file survived.
//
// Only call this on a path claimOutput won for this call. It removes by path,
// so calling it on a path this call did not create would delete another
// caller's take.
func discardOutput(out string, cause error) error {
	if err := os.Remove(out); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("%w (removing %s also failed: %v)", cause, out, err)
	}
	return cause
}

// checkRatio rejects a ratio that one atempo filter cannot carry.
func checkRatio(ratio float64) error {
	if math.IsNaN(ratio) {
		return fmt.Errorf("%w: got NaN", ErrRatioRange)
	}
	if ratio < MinAtempoRatio || ratio > MaxAtempoRatio {
		return fmt.Errorf("%w: got %g, want %g to %g",
			ErrRatioRange, ratio, MinAtempoRatio, MaxAtempoRatio)
	}
	return nil
}

// checkPaths rejects an empty path, a source that is not on disk, an output
// that would overwrite its own input, and an output that already holds a take.
//
// Takes stay immutable, and claimOutput is what enforces that. This function
// stats, so it reports what was true when it looked. A second call can create
// the output between this stat and the render. claimOutput closes that window
// by creating the path exclusively, and it is the reason two racing calls
// cannot both write one take name.
func checkPaths(in, out string) error {
	if in == "" {
		return fmt.Errorf("%w: source take", ErrPathRequired)
	}
	if out == "" {
		return fmt.Errorf("%w: output take", ErrPathRequired)
	}
	if filepath.Clean(in) == filepath.Clean(out) {
		return fmt.Errorf("%w: both name %s", ErrSameFile, out)
	}

	inInfo, inErr := os.Stat(in)
	if inErr != nil {
		if errors.Is(inErr, os.ErrNotExist) {
			return fmt.Errorf("%w: %s", ErrSourceMissing, in)
		}
		return fmt.Errorf("stat source take %s: %w", in, inErr)
	}

	outInfo, err := os.Stat(out)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("stat output take %s: %w", out, err)
	}

	if os.SameFile(inInfo, outInfo) {
		return fmt.Errorf("%w: %s resolves to %s", ErrSameFile, out, in)
	}
	return fmt.Errorf("%w: %s", ErrOutputExists, out)
}
