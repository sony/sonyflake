Example
=======

This example runs Sonyflake on AWS Elastic Beanstalk.

Setup
-----

1. Build the cross compiler for linux/amd64 if using other platforms.

  ```
  cd $GOROOT/src && GOOS=linux GOARCH=amd64 ./make.bash
  ```

2. Build sonyflake_server in the example directory.

  ```
  ./linux64_build.sh
  ```

3. Upload the example directory to AWS Elastic Beanstalk.
