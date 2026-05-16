package rollup

import "time"

// Exported access to a couple of pure helpers so the external _test
// package can drive them without changing the production API surface.

func (s *Scheduler) BucketDate(t time.Time) string  { return s.bucketDate(t) }
func (s *Scheduler) NextFire(now time.Time) time.Time { return s.nextFire(now) }
