package defaultpkg

import (
	"externalhelper"
	t "testing"
)

func helperLoop(b *t.B) {
	for b.Loop() {
		consume(work())
	}
}

func recursiveA(b *t.B) { recursiveB(b) }
func recursiveB(b *t.B) { recursiveA(b) }

func helperMissingLoop(b *t.B) { consume(work()) }

func helperStopTimer(b *t.B) {
	b.StopTimer() // want "reachable iteration path stops timing" "every reachable work statement"
	consume(work())
}

func helperPB(pb *t.PB) {
	for pb.Next() {
		consume(work())
	}
}

func BenchmarkCleanAlias(b *t.B) {
	helperLoop(b)
}

func BenchmarkExternalDelegation(b *t.B) {
	externalhelper.RunBenchmark(b)
}

func BenchmarkLocalHelperMissingLoop(b *t.B) { // want "benchmark scope has no B.Loop"
	helperMissingLoop(b)
}

func BenchmarkLocalHelperTimer(b *t.B) {
	for b.Loop() {
		helperStopTimer(b)
	}
}

func BenchmarkRecursiveDelegation(b *t.B) { // want "benchmark scope has no B.Loop"
	recursiveA(b)
}

func BenchmarkUnknownTimerEffect(b *t.B) {
	for b.Loop() {
		externalhelper.MaybeControlTimer(b)
		consume(work())
	}
}

func BenchmarkMissingLoop(b *t.B) { // want "benchmark scope has no B.Loop"
	consume(work())
}

func BenchmarkNoncanonicalLoop(b *t.B) {
	for b.Loop() == true { // want "B.Loop must be the sole condition"
		consume(work())
	}
}

func BenchmarkMultipleLoop(b *t.B) {
	for b.Loop() {
		consume(work())
	}
	for b.Loop() { // want "only the first testing.B.Loop"
		consume(work())
	}
}

func BenchmarkMixedLoop(b *t.B) {
	for b.Loop() {
		consume(work())
	}
	for range b.N { // want "mixes testing.B.Loop with b.N"
		consume(work())
	}
}

func BenchmarkBNReadBeforeLoop(b *t.B) {
	consume(b.N) // want "b.N is only guaranteed"
	for b.Loop() {
		consume(work())
	}
}

func BenchmarkBNReadAfterLoop(b *t.B) {
	for b.Loop() {
		consume(work())
	}
	b.ReportMetric(float64(b.N), "items")
}

func BenchmarkWrongBNCount(b *t.B) {
	for i := 0; i <= b.N; i++ { // want "provably not executed exactly b.N times"
		consume(i)
	}
}

func BenchmarkWrongBNStart(b *t.B) {
	for i := 1; i < b.N; i++ { // want "provably not executed exactly b.N times"
		consume(i)
	}
}

func BenchmarkWrongBNStep(b *t.B) {
	for i := 0; i < b.N; i += 2 { // want "provably not executed exactly b.N times"
		consume(i)
	}
}

func BenchmarkCanonicalBNNotEqual(b *t.B) {
	for i := 0; i != b.N; i++ {
		consume(i)
	}
}

func BenchmarkResetInLoop(b *t.B) {
	for b.Loop() {
		b.ResetTimer() // want "ResetTimer inside a measured iteration"
		consume(work())
	}
}

func BenchmarkStopWithoutStart(b *t.B) {
	for b.Loop() {
		b.StopTimer() // want "reachable iteration path stops timing" "every reachable work statement"
		consume(work())
	}
}

func BenchmarkWorkStopped(b *t.B) {
	for b.Loop() {
		b.StopTimer() // want "reachable iteration path stops timing" "every reachable work statement"
		consume(work())
	}
}

func BenchmarkTimerBranchClean(b *t.B) {
	for b.Loop() {
		b.StopTimer()
		if work() > 0 {
			b.StartTimer()
		} else {
			b.StartTimer()
		}
		consume(work())
	}
}

func BenchmarkTimerBranchMissingStart(b *t.B) {
	for b.Loop() {
		b.StopTimer() // want "reachable iteration path stops timing"
		if work() > 0 {
			b.StartTimer()
		}
		consume(work())
	}
}

func BenchmarkTimerLoopBackedge(b *t.B) {
	for b.Loop() {
		for work() > 0 {
			b.StopTimer() // want "reachable iteration path stops timing"
			break
		}
		consume(work())
	}
}

func BenchmarkSubbenchOuter(b *t.B) {
	b.Run("child", func(child *t.B) { // want "benchmark scope has no B.Loop"
		for b.Loop() { // want "subbenchmark callback uses the parent"
			consume(work())
		}
	})
}

func BenchmarkSubbenchClean(b *t.B) {
	b.Run("child", func(child *t.B) {
		for child.Loop() {
			consume(work())
		}
	})
}

func BenchmarkRunParallelMissingNext(b *t.B) {
	b.RunParallel(func(pb *t.PB) { // want "neither iterates with pb.Next"
		consume(work())
	})
}

func BenchmarkRunParallelWrongLoop(b *t.B) {
	b.RunParallel(func(pb *t.PB) { // want "neither iterates with pb.Next"
		for range b.N { // want "must iterate with pb.Next"
			consume(work())
		}
	})
}

func BenchmarkRunParallelRepeatedNext(b *t.B) {
	b.RunParallel(func(pb *t.PB) {
		for pb.Next() {
			consume(work())
		}
		for pb.Next() { // want "more than one pb.Next loop"
			consume(work())
		}
	})
}

func BenchmarkRunParallelTimer(b *t.B) {
	b.RunParallel(func(pb *t.PB) {
		b.ResetTimer() // want "timer methods have global effect"
		for pb.Next() {
			consume(work())
		}
	})
}

func BenchmarkRunParallelSubbenchmark(b *t.B) {
	b.RunParallel(func(pb *t.PB) {
		b.Run("nested", func(child *t.B) { // want "must not start a subbenchmark"
			for child.Loop() {
				consume(work())
			}
		})
		for pb.Next() {
			consume(work())
		}
	})
}

func BenchmarkRunParallelClean(b *t.B) {
	b.SetParallelism(2)
	b.RunParallel(func(pb *t.PB) {
		local := 0
		for pb.Next() {
			local += work()
		}
		consume(local)
	})
}

func BenchmarkRunParallelExternalDelegation(b *t.B) {
	b.RunParallel(func(pb *t.PB) {
		externalhelper.RunParallel(pb)
	})
}

func BenchmarkRunParallelLocalHelper(b *t.B) {
	b.RunParallel(func(pb *t.PB) {
		helperPB(pb)
	})
}

func BenchmarkSetParallelismOrder(b *t.B) {
	b.RunParallel(func(pb *t.PB) {
		for pb.Next() {
			consume(work())
		}
	})
	b.SetParallelism(2) // want "definitely executes after RunParallel"
}

func BenchmarkSetParallelismBranchClean(b *t.B) {
	if work() > 0 {
		b.RunParallel(func(pb *t.PB) {
			for pb.Next() {
				consume(work())
			}
		})
	} else {
		b.SetParallelism(2)
	}
}

func BenchmarkSetParallelismBranchWrong(b *t.B) {
	b.RunParallel(func(pb *t.PB) {
		for pb.Next() {
			consume(work())
		}
	})
	if work() > 0 {
		b.SetParallelism(2) // want "definitely executes after RunParallel"
	}
}

func BenchmarkMalformedAttempt(b *t.B) {
	for b.Loop() && work() > 0 { // want "B.Loop must be the sole condition"
		consume(work())
	}
}

func BenchmarkReceiverAlias(b *t.B) {
	alias := b
	for alias.Loop() {
		consume(work())
	}
}

func BenchmarkShadowedReceiver(b *t.B) {
	for b.Loop() {
		b := &fakeB{}
		if b.Loop() {
			consume(b.N)
		}
	}
}

// Fake testing-like names must not be treated as the real testing API.
type fakeB struct{ N int }

func (b *fakeB) Loop() bool { return false }

func NotABenchmarkFakeType(b *fakeB) {}
