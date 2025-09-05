package types

// Stringer method for Params.
// func (p Status) String() string {
// 	bz, err := json.Marshal(p)
// 	if err != nil {
// 		panic(err)
// 	}

// 	return string(bz)
// }

// TODO: update the validation fn
// Validate does the sanity check on the params.
func (p Status) ValidateBasic() error {
	return nil
}
