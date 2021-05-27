package mr

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"log"
	"net/rpc"
	"os"
	"sort"
	"strconv"
	"time"
)

//
// Map functions return a slice of KeyValue.
//
type KeyValue struct {
	Key   string
	Value string
}

// for sorting by key.
type ByKey []KeyValue

// for sorting by key.
func (a ByKey) Len() int           { return len(a) }
func (a ByKey) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a ByKey) Less(i, j int) bool { return a[i].Key < a[j].Key }

//
// use ihash(key) % NReduce to choose the reduce
// task number for each KeyValue emitted by Map.
//
func ihash(key string) int {
	h := fnv.New32a()
	h.Write([]byte(key))
	return int(h.Sum32() & 0x7fffffff)
}
func genWorkerID() (uuid string) {
	// generate 32 bits timestamp
	unix32bits := uint32(time.Now().UTC().Unix())

	buff := make([]byte, 12)

	numRead, err := rand.Read(buff)

	if numRead != len(buff) || err != nil {
		panic(err)
	}
	return fmt.Sprintf("%x-%x-%x-%x-%x-%x\n", unix32bits, buff[0:2], buff[2:4], buff[4:6], buff[6:8], buff[8:])
}

//
// main/mrworker.go calls this function.
//
func Worker(mapf func(string, string) []KeyValue,
	reducef func(string, []string) string) {

	args := Args{WorkerId: genWorkerID()}
	reply := Reply{}
	// send the RPC request, wait for the reply.
	call("Master.SendTask", &args, &reply)
	// 如果任务一直有效，则一直干活
	for reply.Valid {
		workArgs := WorkStatus{
			WorkerId: args.WorkerId,
			WorkType: reply.WorkType,
			Done:     false,
		}
		if reply.WorkType == "map" {
			if MapWork(mapf, reply) {
				workArgs.Done = true
			}
			call("Master.ReceiveStatus", &workArgs, &reply)
		} else {
			if ReduceWork(reducef, reply) {
				workArgs.Done = true
			}
			call("Master.ReceiveStatus", &workArgs, &reply)
		}
		reply = Reply{}
		call("Master.SendTask", &args, &reply)
		fmt.Println("Work is valid? ", reply.Valid)
	}
}
func MapWork(mapf func(string, string) []KeyValue, reply Reply) bool {
	mapf(reply.Filename, reply.Content)
	kvs := mapf(reply.Filename, reply.Content)
	// 二维的kv, 即 nReduce 个桶
	reduces := make([][]KeyValue, reply.NReduce)
	// 将结果分到 nReduce 个桶中
	for _, kv := range kvs {
		idx := ihash(kv.Key) % reply.NReduce
		reduces[idx] = append(reduces[idx], kv)
	}
	for idx, reduce := range reduces {
		file := fmt.Sprintf("mr-%v-%v", reply.Id, idx)
		_, err := os.Stat(file)
		var f *os.File
		if os.IsExist(err) {
			if f, err = os.Open(file); err != nil {
				fmt.Println(err)
				return false
			}
		} else {
			if f, err = os.Create(file); err != nil {
				fmt.Println(err)
				return false
			}
		}
		enc := json.NewEncoder(f)
		for _, kv := range reduce {
			if err := enc.Encode(&kv); err != nil {
				fmt.Println("error in encode")
			}
		}
	}
	log.Println("Map execute succeed")
	return true
}
func ReduceWork(reducef func(string, []string) string, reply Reply) bool {
	var intermediate []KeyValue
	for n := 0; n < reply.NMap; n++ {
		fileName := "mr-" + strconv.Itoa(n) + "-" + strconv.Itoa(reply.Id)
		f, err := os.Open(fileName)
		defer f.Close()
		if err != nil {
			fmt.Println("unable to read ", fileName)
		}
		decoder := json.NewDecoder(f)
		var kv KeyValue
		for decoder.More() {
			if err := decoder.Decode(&kv); err != nil {
				log.Println("Json decode failed, ", err)
			}
			intermediate = append(intermediate, kv)
		}

	}
	sort.Sort(ByKey(intermediate))
	i := 0
	ofile, err := os.Create("mr-out-" + strconv.Itoa(reply.Id))
	if err != nil {
		log.Println("Unable to create file, ", err)
		return false
	}
	for i < len(intermediate) {
		j := i + 1
		for j < len(intermediate) && intermediate[j].Key == intermediate[i].Key {
			j++
		}
		var values []string
		for k := i; k < j; k++ {
			values = append(values, intermediate[k].Value)
		}
		output := reducef(intermediate[i].Key, values)
		// this is the correct format for each line of Reduce output.
		fmt.Fprintf(ofile, "%v %v\n", intermediate[i].Key, output)
		i = j
	}
	defer ofile.Close()

	log.Println("Reduce execute succeed")
	return true
}

//
// example function to show how to make an RPC call to the master.
//
// the RPC argument and reply types are defined in rpc.go.
//
func CallExample() {

	// declare an argument structure.
	args := ExampleArgs{}

	// fill in the argument(s).
	args.X = 99

	// declare a reply structure.
	reply := ExampleReply{}

	// send the RPC request, wait for the reply.
	call("Master.Example", &args, &reply)

	// reply.Y should be 100.
	fmt.Printf("reply.Y %v\n", reply.Y)
}

//
// send an RPC request to the master, wait for the response.
// usually returns true.
// returns false if something goes wrong.
//
func call(rpcname string, args interface{}, reply interface{}) bool {
	//c, err := rpc.DialHTTP("tcp", "127.0.0.1"+":1234")
	sockname := masterSock()
	c, err := rpc.DialHTTP("unix", sockname)
	if err != nil {
		log.Fatal("dialing:", err)
	}
	defer c.Close()

	err = c.Call(rpcname, args, reply)
	if err == nil {
		return true
	}

	fmt.Println(err)
	return false
}
