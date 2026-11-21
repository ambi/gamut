package operators

import (
	"errors"
	"testing"
)

func TestSelfAssignments(t *testing.T) {
	if Accumulate(0, 3) != 3 {
		t.Fatal("wrong")
	}
}

func TestNumericLiterals(t *testing.T) {
	if Limit() != 3 || Rate() != 0.5 {
		t.Fatal("wrong")
	}
	_, _ = Retries(), Ratio()
}

func TestBranches(t *testing.T) {
	if Abs(-2) != 2 || Note(1) != 1 || Sign(1) != 1 || Level(1) != "low" || Name(1) != "one" || Kind(2) != "even" {
		t.Fatal("wrong")
	}
}

func TestErrors(t *testing.T) {
	if !errors.Is(Wrap(), ErrBase) || Annotate().Error() != "annotate: base" || Check(-1) == nil {
		t.Fatal("wrong")
	}
	_ = Validate(200)
}

func TestBitwise(t *testing.T) {
	if Flags(1, 2) != 3 || Mask(3, 3) != 3 || Double(2) != 4 {
		t.Fatal("wrong")
	}
}

func TestLogical(t *testing.T) {
	if Both(true, false) || !Either(true, true) {
		t.Fatal("wrong")
	}
}

func TestLiterals(t *testing.T) {
	if !Enabled() {
		t.Fatal("wrong")
	}
	_ = Verbose()
}

func TestConditions(t *testing.T) {
	if !Positive(1) || Positive(-1) || SumAll(nil) != 0 {
		t.Fatal("wrong")
	}
}

func TestReturns(t *testing.T) {
	if Area(2, 3) != 6 {
		t.Fatal("wrong")
	}
	_ = Describe(1)
}

func TestStatements(t *testing.T) {
	// パッケージ変数を戻してから使う。戻さないと、2 回目の実行（go test -count=2 や、gamut の使い回すテストプロセス）で結果が変わる。
	recorded = nil
	Record("a")
	if len(recorded) != 1 {
		t.Fatal("wrong")
	}
	Notify()
}

func TestNot(t *testing.T) {
	if !Closed(false) {
		t.Fatal("wrong")
	}
	_ = Muted(true)
}

func TestLoopControl(t *testing.T) {
	if FirstNegative([]int{1, -2, -3}) != -2 || CountPositive([]int{1, 2, 0}) != 2 {
		t.Fatal("wrong")
	}
}

func TestAssignments(t *testing.T) {
	if Total([]int{1, 2}) != 3 || Scaled(5, 1) != 5 || SetBit(2) != 3 || ClearHigh(0) != 0 {
		t.Fatal("wrong")
	}
}
