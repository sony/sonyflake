# Sonyflake

[![GoDoc](https://pkg.go.dev/badge/github.com/sony/sonyflake/v2?utm_source=godoc)](https://pkg.go.dev/github.com/sony/sonyflake/v2)
[![Go Report Card](https://goreportcard.com/badge/github.com/sony/sonyflake/v2)](https://goreportcard.com/report/github.com/sony/sonyflake/v2)

Sonyflake is a distributed unique ID generator inspired by [Twitter's Snowflake](https://blog.twitter.com/2010/announcing-snowflake).  

Sonyflake focuses on lifetime and performance on many host/core environment.
So it has a different bit assignment from Snowflake.
By default, a Sonyflake ID is composed of

    39 bits for time in units of 10 msec
     8 bits for a sequence number
    16 bits for a machine id

As a result, Sonyflake has the following advantages and disadvantages:

- The lifetime (174 years) is longer than that of Snowflake (69 years)
- It can work in more distributed machines (2^16) than Snowflake (2^10)
- It can generate 2^8 IDs per 10 msec at most in a single instance (fewer than Snowflake)

However, if you want more generation rate in a single host,
you can easily run multiple Sonyflake instances parallelly using goroutines.

In addition, you can adjust the lifetime and generation rate of Sonyflake
by customizing the bit assignment and the time unit.

## Installation

```
go get github.com/sony/sonyflake/v2
```

## Usage

The function New creates a new Sonyflake instance.

```go
func New(st Settings) (*Sonyflake, error)
```

You can configure Sonyflake by the struct Settings:

```go
type Settings struct {
	BitsSequence   int
	BitsMachineID  int
	TimeUnit       time.Duration
	StartTime      time.Time
	MachineID      func() (int, error)
	CheckMachineID func(int) bool
}
```

- BitsSequence is the bit length of a sequence number.
  If BitsSequence is 0, the default bit length is used, which is 8.
  If BitsSequence is 31 or more, an error is returned.

- BitsMachineID is the bit length of a machine ID.
  If BitsMachineID is 0, the default bit length is used, which is 16.
  If BitsMachineID is 31 or more, an error is returned.

- TimeUnit is the time unit of Sonyflake.
  If TimeUnit is 0, the default time unit is used, which is 10 msec.
  TimeUnit must be 1 msec or longer.

- StartTime is the time since which the Sonyflake time is defined as the elapsed time.
  If StartTime is 0, the start time of the Sonyflake instance is set to "2025-01-01 00:00:00 +0000 UTC".
  StartTime must be before the current time.

- MachineID returns the unique ID of a Sonyflake instance.
  If MachineID returns an error, the instance will not be created.
  If MachineID is nil, the default MachineID is used, which returns the lower 16 bits of the private IP address.

- CheckMachineID validates the uniqueness of a machine ID.
  If CheckMachineID returns false, the instance will not be created.
  If CheckMachineID is nil, no validation is done.

The bit length of time is calculated by 63 - BitsSequence - BitsMachineID.
If it is less than 32, an error is returned.

In order to get a new unique ID, you just have to call the method NextID.

```go
func (sf *Sonyflake) NextID() (int64, error)
```

NextID can continue to generate IDs for about 174 years from StartTime by default.
But after the Sonyflake time is over the limit, NextID returns an error.

## AWS VPC and Docker

The [awsutil](https://github.com/sony/sonyflake/blob/master/v2/awsutil) package provides
the function AmazonEC2MachineID that returns the lower 16-bit private IP address of the Amazon EC2 instance.
It also works correctly on Docker
by retrieving [instance metadata](http://docs.aws.amazon.com/en_us/AWSEC2/latest/UserGuide/ec2-instance-metadata.html).

[AWS IPv4 VPC](https://docs.aws.amazon.com/vpc/latest/userguide/vpc-cidr-blocks.html)
is usually assigned a single CIDR with a netmask between /28 and /16.
So if each EC2 instance has a unique private IP address in AWS VPC,
the lower 16 bits of the address is also unique.
In this common case, you can use AmazonEC2MachineID as Settings.MachineID.

See [example](https://github.com/sony/sonyflake/blob/master/v2/example) that runs Sonyflake on AWS Elastic Beanstalk.

## License

The MIT License (MIT)

See [LICENSE](https://github.com/sony/sonyflake/blob/master/LICENSE) for details.
