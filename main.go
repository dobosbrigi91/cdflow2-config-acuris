package main

import (
	"fmt"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/sts"
	"log"

	"github.com/mergermarket/cdflow2-config-acuris/internal/handler"
	common "github.com/mergermarket/cdflow2-config-common"
)

func main() {
	handler := handler.New(&handler.Opts{
		STSClient: sts.New(session.New()),
	})
	request := common.CreateConfigureReleaseRequest()
	request.Team = "ninenine"
	request.ReleaseRequiredEnv = map[string][]string{
		"docker": []string{},
	}
	response := common.CreateConfigureReleaseResponse()
	if err := handler.ConfigureRelease(request, response); err != nil {
		log.Fatal(err)
	}
	fmt.Println(response)
	/*
		if len(os.Args) == 2 && os.Args[1] == "forward" {
			common.Forward(os.Stdin, os.Stdout, "")
		} else {
			common.Listen(handler.New(&handler.Opts{}), "", nil)
		}
	*/
}
