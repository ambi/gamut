package operators

import "testing"

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
