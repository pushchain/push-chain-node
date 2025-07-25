// Code generated by MockGen. DO NOT EDIT.
// Source: x/ue/types/expected_keepers.go

// Package mocks is a generated GoMock package.
package mocks

import (
	big "math/big"
	reflect "reflect"

	types "github.com/cosmos/cosmos-sdk/types"
	statedb "github.com/cosmos/evm/x/vm/statedb"
	types0 "github.com/cosmos/evm/x/vm/types"
	abi "github.com/ethereum/go-ethereum/accounts/abi"
	common "github.com/ethereum/go-ethereum/common"
	gomock "github.com/golang/mock/gomock"
)

// MockEVMKeeper is a mock of EVMKeeper interface.
type MockEVMKeeper struct {
	ctrl     *gomock.Controller
	recorder *MockEVMKeeperMockRecorder
}

// MockEVMKeeperMockRecorder is the mock recorder for MockEVMKeeper.
type MockEVMKeeperMockRecorder struct {
	mock *MockEVMKeeper
}

// NewMockEVMKeeper creates a new mock instance.
func NewMockEVMKeeper(ctrl *gomock.Controller) *MockEVMKeeper {
	mock := &MockEVMKeeper{ctrl: ctrl}
	mock.recorder = &MockEVMKeeperMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockEVMKeeper) EXPECT() *MockEVMKeeperMockRecorder {
	return m.recorder
}

// CallEVM mocks base method.
func (m *MockEVMKeeper) CallEVM(ctx types.Context, abi abi.ABI, from, contract common.Address, commit bool, method string, args ...interface{}) (*types0.MsgEthereumTxResponse, error) {
	m.ctrl.T.Helper()
	varargs := []interface{}{ctx, abi, from, contract, commit, method}
	for _, a := range args {
		varargs = append(varargs, a)
	}
	ret := m.ctrl.Call(m, "CallEVM", varargs...)
	ret0, _ := ret[0].(*types0.MsgEthereumTxResponse)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// CallEVM indicates an expected call of CallEVM.
func (mr *MockEVMKeeperMockRecorder) CallEVM(ctx, abi, from, contract, commit, method interface{}, args ...interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	varargs := append([]interface{}{ctx, abi, from, contract, commit, method}, args...)
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "CallEVM", reflect.TypeOf((*MockEVMKeeper)(nil).CallEVM), varargs...)
}

// DerivedEVMCall mocks base method.
func (m *MockEVMKeeper) DerivedEVMCall(ctx types.Context, abi abi.ABI, from, contract common.Address, value, gasLimit *big.Int, commit, gasless bool, method string, args ...interface{}) (*types0.MsgEthereumTxResponse, error) {
	m.ctrl.T.Helper()
	varargs := []interface{}{ctx, abi, from, contract, value, gasLimit, commit, gasless, method}
	for _, a := range args {
		varargs = append(varargs, a)
	}
	ret := m.ctrl.Call(m, "DerivedEVMCall", varargs...)
	ret0, _ := ret[0].(*types0.MsgEthereumTxResponse)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// DerivedEVMCall indicates an expected call of DerivedEVMCall.
func (mr *MockEVMKeeperMockRecorder) DerivedEVMCall(ctx, abi, from, contract, value, commit, gasless, method interface{}, args ...interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	varargs := append([]interface{}{ctx, abi, from, contract, value, commit, gasless, method}, args...)
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "DerivedEVMCall", reflect.TypeOf((*MockEVMKeeper)(nil).DerivedEVMCall), varargs...)
}

// SetAccount mocks base method.
func (m *MockEVMKeeper) SetAccount(ctx types.Context, addr common.Address, account statedb.Account) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "SetAccount", ctx, addr, account)
	ret0, _ := ret[0].(error)
	return ret0
}

// SetAccount indicates an expected call of SetAccount.
func (mr *MockEVMKeeperMockRecorder) SetAccount(ctx, addr, account interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "SetAccount", reflect.TypeOf((*MockEVMKeeper)(nil).SetAccount), ctx, addr, account)
}

// SetCode mocks base method.
func (m *MockEVMKeeper) SetCode(ctx types.Context, codeHash, code []byte) {
	m.ctrl.T.Helper()
	m.ctrl.Call(m, "SetCode", ctx, codeHash, code)
}

// SetCode indicates an expected call of SetCode.
func (mr *MockEVMKeeperMockRecorder) SetCode(ctx, codeHash, code interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "SetCode", reflect.TypeOf((*MockEVMKeeper)(nil).SetCode), ctx, codeHash, code)
}

// SetState mocks base method.
func (m *MockEVMKeeper) SetState(ctx types.Context, addr common.Address, key common.Hash, value []byte) {
	m.ctrl.T.Helper()
	m.ctrl.Call(m, "SetState", ctx, addr, key, value)
}

// SetState indicates an expected call of SetState.
func (mr *MockEVMKeeperMockRecorder) SetState(ctx, addr, key, value interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "SetState", reflect.TypeOf((*MockEVMKeeper)(nil).SetState), ctx, addr, key, value)
}
