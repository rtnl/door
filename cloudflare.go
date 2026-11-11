package main

import (
	"encoding/json"
	"fmt"
	"net/http"
)

const (
	CloudflareApiEndpoint = "https://api.cloudflare.com/client/v4/ips"
	UserAgent             = "rtnl-door/0.1"
)

type CloudflareApiResponse struct {
	Success bool `json:"success"`
	Result  any  `json:"result"`
	// errors: []
	// messages: []
}

type CloudflareApiIps struct {
	Ipv4CidrList []string `json:"ipv4_cidrs"`
	Ipv6CidrList []string `json:"ipv6_cidrs"`
	Etag         string   `json:"etag"`
}

func PullCloudflareApiIps() (result *CloudflareApiIps, err error) {
	req, err := http.NewRequest(http.MethodGet, CloudflareApiEndpoint, nil)
	if err != nil {
		return
	}

	req.Header.Add("User-Agent", UserAgent)

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return
	}

	decoder := json.NewDecoder(res.Body)
	response := new(CloudflareApiResponse)
	err = decoder.Decode(response)
	if err != nil {
		return
	}

	if !response.Success {
		err = fmt.Errorf("CloudflareApiResponse success is false")
		return
	}

	data, err := json.Marshal(response.Result)
	if err != nil {
		return
	}

	err = json.Unmarshal(data, &result)
	if err != nil {
		return
	}

	return
}
