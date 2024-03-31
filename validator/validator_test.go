package validator

import "testing"

func TestGetSignInfo(t *testing.T) {
	validator := New("http://127.0.0.1:1317/")
	signInfo, err := validator.getSignInfo("0af6ceb4-b0b7-4b0c-9e20-9eaa3bf7dbcd")
	if err != nil {
		t.Fatal(err)
	}

	t.Logf("signInfo:%v", *signInfo)
}
