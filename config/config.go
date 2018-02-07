package config

import (
	"encoding/json"
	"io/ioutil"
	"os"
	"strconv"
	"github.com/sirupsen/logrus"
)

var ConfigSettings map[string]string


// LoadConfig reads the given file parsing it and returning a
// ConfigSetting splice
func LoadConfig(fileName string) {

	ConfigSettings = make(map[string]string)
	file, e := ioutil.ReadFile(fileName)
	if e != nil {
		logrus.Error("File error: %v\n", e)
		os.Exit(1)
	}
	json.Unmarshal(file, &ConfigSettings)
}


func GetConfigValueAsFloat64(name string) *float64 {

	if ConfigSettings != nil{
		if val, ok := ConfigSettings[name]; ok {
			res, err := strconv.ParseFloat(val, 64)
			if err != nil {
				return nil
			}
			return &res
		}
	}
	return nil
}

func GetConfigValueAsInt64(name string) *int64{

	if ConfigSettings != nil{
		if val, ok := ConfigSettings[name]; ok {
			res, err := strconv.ParseInt(val, 10,64)
			if err != nil {
				return nil
			}
			return &res
		}
	}
	return nil
}
