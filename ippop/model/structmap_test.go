package model

import (
	"testing"
	"time"
)

func TestStructToMap(t *testing.T) {
	type MyType struct {
		Id      string
		Name    string    `redis:"name"`
		Abc     string    `redis:"abc"`
		Online  bool      `redis:"online"`
		LoginAt time.Time `redis:"loginAt"`
		CPU     int32     `redis:"cpu"`
		Memory  float32   `redis:"memroy"`
	}

	myType := MyType{
		Id:      "aaa_bbb_ccc",
		Name:    "my-name-is-abc",
		Abc:     "abc",
		Online:  true,
		LoginAt: time.Now(),
		CPU:     4,
		Memory:  100.01,
	}
	m, err := structToMap(myType)
	if err != nil {
		t.Fatalf("structToMap failed:%s", err.Error())
	}

	t.Logf("%#v", m)

	myType1 := &MyType{}
	err = mapToStruct(m, myType1)
	if err != nil {
		t.Fatalf("mapToStruct failed:%s", err.Error())
	}

	t.Logf("myType1 %#v", myType1)
}
