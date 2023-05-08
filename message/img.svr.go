package message

import (
	"io/ioutil"
	"log"
	"os"
)

type ImgService struct{}

func (this *ImgService) Get(arg *ImgRequest, reply *ImgResponse) error {
	file, err := os.Open(arg.Name)
	if err != nil {
		log.Fatal(err)
		return err
	}
	defer file.Close()
	data, err := ioutil.ReadAll(file)
	if err != nil {
		log.Fatal(err)
		return err
	}
	reply.Data = data
	return nil
}
