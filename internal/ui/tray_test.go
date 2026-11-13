package ui

import "testing"

func TestTrayImageClosestSize(t *testing.T) {
	for _, tc := range []struct{ size, want int }{{16, 16}, {28, 28}, {48, 48}, {12, 16}, {30, 28}, {44, 40}, {96, 48}} {
		img, err := trayImage(tc.size)
		if err != nil {
			t.Fatalf("trayImage(%d): %v", tc.size, err)
		}
		if got := img.Bounds().Dx(); got != tc.want {
			t.Errorf("trayImage(%d) is %d pixels, want %d", tc.size, got, tc.want)
		}
	}
}
