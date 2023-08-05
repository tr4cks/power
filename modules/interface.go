package modules

type Module interface {
	Init(config map[string]interface{}) error
	State() (Result[bool] /* power */, Result[bool] /* led */)
	PowerOn() error
	PowerOff() error
}

type DefaultModule struct{}

func (*DefaultModule) Init(config map[string]interface{}) error {
	return nil
}

func (*DefaultModule) State() (Result[bool], Result[bool]) {
	return Result[bool]{}, Result[bool]{}
}

func (*DefaultModule) PowerOn() error {
	return nil
}

func (*DefaultModule) PowerOff() error {
	return nil
}
