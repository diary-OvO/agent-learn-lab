package main

import (
	"AgentLoop/mini_agent_loop/openai/tools"
	v1 "AgentLoop/mini_agent_loop/openai/tools/v1"

	"github.com/openai/openai-go/v3"
)

func main() {
	client := openai.NewClient()

	//先注册工具运行的box
	toolbox := v1.NewToolBox(
		tools.NewWeatherToolV1())

	//再注册传给LLM的函数
	var a openai.ChatC

}
