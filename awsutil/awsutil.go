// Package awsutil provides utility functions for using Sonyflake on AWS.
package awsutil

import (
	"errors"
	"io/ioutil"
	"net"
	"net/http"
	"os/exec"
	"regexp"
	"strconv"
	"time"
)

func amazonEC2PrivateIPv4() (net.IP, error) {
	res, err := http.Get("http://169.254.169.254/latest/meta-data/local-ipv4")
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	body, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}

	ip := net.ParseIP(string(body))
	if ip == nil {
		return nil, errors.New("invalid ip address")
	}
	return ip.To4(), nil
}

// AmazonEC2MachineID retrieves the private IP address of the Amazon EC2 instance
// and returns its lower 16 bits.
// It works correctly on Docker as well.
func AmazonEC2MachineID() (uint16, error) {
	ip, err := amazonEC2PrivateIPv4()
	if err != nil {
		return 0, err
	}

	return uint16(ip[2])<<8 + uint16(ip[3]), nil
}

// TimeDifference returns the time difference between the localhost and the given NTP server.
func TimeDifference(server string) (time.Duration, error) {
	output, err := exec.Command("/usr/sbin/ntpdate", "-q", server).CombinedOutput()
	if err != nil {
		return time.Duration(0), err
	}

	re, _ := regexp.Compile("offset (.*) sec")
	submatched := re.FindSubmatch(output)
	if len(submatched) != 2 {
		return time.Duration(0), errors.New("invalid ntpdate output")
	}

	f, err := strconv.ParseFloat(string(submatched[1]), 64)
	if err != nil {
		return time.Duration(0), err
	}
	return time.Duration(f*1000) * time.Millisecond, nil
}
