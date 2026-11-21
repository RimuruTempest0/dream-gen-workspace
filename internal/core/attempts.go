package core

import (
	"sort"
	"time"
)

// earlier orders stored times (RFC 3339, of differing precision) by the moment
// they name; a time that does not parse is compared as text.
func earlier(a, b string) bool {
	x, e1 := time.Parse(time.RFC3339Nano, a)
	y, e2 := time.Parse(time.RFC3339Nano, b)
	if e1 != nil || e2 != nil {
		return a < b
	}
	return x.Before(y)
}

// A slot of a 套图 (a plan item) can have several attempts: retries, automatic
// resends, sample redraws and refinements. Its current attempt is the newest.

// attemptBefore orders attempts by the moment they were created, not by the
// stored text: stored times drop trailing fractional zeros, so ".5Z" sorts
// after ".55Z" as text. At the same moment a refinement comes after the image
// it was refined from.
func attemptBefore(x, y Task) bool {
	return earlier(x.CreatedAt, y.CreatedAt) || (!earlier(y.CreatedAt, x.CreatedAt) && y.RefinedFrom == x.ID)
}

// sortNewestFirst orders tasks newest attempt first, so the first task of a
// slot is its current attempt. Tasks of the same moment keep their order.
func sortNewestFirst(tasks []Task) {
	sort.SliceStable(tasks, func(i, j int) bool { return attemptBefore(tasks[j], tasks[i]) })
}

// suitePlanOf is the plan a suite was generated from: the submitted revision,
// else the newest. It is false when the suite saved no plan.
func suitePlanOf(plans []SuitePlan, submittedRevision int) (SuitePlan, bool) {
	if len(plans) == 0 {
		return SuitePlan{}, false
	}
	if submittedRevision > 0 && submittedRevision <= len(plans) {
		return plans[submittedRevision-1], true
	}
	return plans[len(plans)-1], true
}
