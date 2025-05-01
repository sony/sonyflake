package main

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/sony/sonyflake/v2"
	"github.com/sony/sonyflake/v2/awsutil"
)

var sf *sonyflake.Sonyflake

func init() {
	var st sonyflake.Settings
	st.MachineID = awsutil.AmazonEC2MachineID
	var err error
sf, err = sonyflake.New(st)
if err != nil {
	log.Fatal(err)
}
	if sf == nil {
		panic("sonyflake not created")
	}
}

func handler(w http.ResponseWriter, r *http.Request) {
	id, err := sf.NextID()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	body, err := json.Marshal(sonyflake.Decompose(id))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header()["Content-Type"] = []string{"application/json; charset=utf-8"}
	_, err = w.Write(body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func main() {
	http.HandleFunc("/", handler)
	err := http.ListenAndServe(":8080", nil)
	if err != nil {
		panic(err)
	}
}
