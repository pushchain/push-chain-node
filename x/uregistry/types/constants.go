package types

var GATEWAY_METHOD = struct {
	SVM struct {
		AddFunds string
	}
	EVM struct {
		AddFunds string
	}
}{
	SVM: struct{ AddFunds string }{AddFunds: "add_funds"},
	EVM: struct{ AddFunds string }{AddFunds: "addFunds"},
}
