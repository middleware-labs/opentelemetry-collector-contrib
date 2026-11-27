package report

import "testing"

func TestUptimeWrapIsNotAReboot(t *testing.T) {
	const wrap = float64(1 << 32)
	if !uptimeWrapped(wrap-100, 50) {
		t.Error("a 32-bit sysUpTime wrap was taken for a reboot")
	}
	if uptimeWrapped(360000, 100) {
		t.Error("a reboot after one hour was taken for a wrap")
	}
}

func TestPreferredDeviceName(t *testing.T) {
	for i := 0; i < 20; i++ {
		if got := preferredDeviceName(map[string]string{"sys_name": "b", "name": "a"}); got != "a" {
			t.Fatalf("name = %q, want the name field over sys_name", got)
		}
	}
	if got := preferredDeviceName(map[string]string{"sys_name": "b"}); got != "b" {
		t.Errorf("name = %q, want sys_name as the fallback", got)
	}
}
