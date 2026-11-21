package calc

import "testing"

// TestPanicky は、失敗を panic で知らせる。プロセスが落ちても、後のテストの検出も記録されなければならない。
func TestPanicky(t *testing.T) {
	if Add(2, 3) != 5 {
		panic("wrong sum")
	}
}

// TestNothing は表明を持たない。
func TestNothing(t *testing.T) {
	_ = Add(1, 2)
	_ = Sub(1, 2)
}

// TestAdd と TestAddAgain は同じ変異体を検出し、どちらも TestBoth に包含される。
func TestAdd(t *testing.T) {
	if Add(2, 3) != 5 {
		t.Fatal("wrong sum")
	}
}

func TestAddAgain(t *testing.T) {
	if Add(4, 1) != 5 {
		t.Fatal("wrong sum")
	}
}

func TestBoth(t *testing.T) {
	if Add(2, 3) != 5 || Sub(5, 3) != 2 {
		t.Fatal("wrong")
	}
}
