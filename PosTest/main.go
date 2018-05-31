package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net"
	"strconv"
	"sync"
	"time"
	"github.com/joho/godotenv"
	"github.com/davecgh/go-spew/spew"
	"os"
)

//区块
type Block struct {

	//区块ID
	Index int
	//时间戳
	Timestamp string
	//每个验证者的人体脉搏
	BMP int
	//当前区块的hash值
	Hash string
	//上一个区块的Hash值
	PrevHash string
	//
	Validator string
}

var Blockchain []Block
var tempBlocks []Block

//候选者通道
var candidateBlocks = make(chan Block)

//我们主Go TCP服务器将向所有节点广播最新的区块链
var announcements = make(chan string)

//锁的声明，取地址是因为很多的地方都要用锁
var mutex = &sync.Mutex{}

//是节点的存储map，同时也会保存每个节点持有的令牌数
var validators = make(map[string]int)

func main() {

	err:=godotenv.Load()
	if err != nil{
		log.Fatal(err)
	}

	//创建初始区块
	t:= time.Now()
	genesisBlock:= Block{}
	genesisBlock = Block{0,t.String(),0,calculateBlockHash(genesisBlock),"",""}
	spew.Dump(genesisBlock)
	Blockchain = append(Blockchain,genesisBlock)
	httpPort:=os.Getenv("PORT")

	//启动TCP服务
	server,err := net.Listen("tcp",":"+httpPort)
	if err!= nil {
		log.Fatal(err)
	}
	log.Println("Http Server Listening On Port:",httpPort)
	defer server.Close()

	//启动一个Go routine 从candidateBlock 通道中获取提议的区块，然后填充到临时缓存中

	go func() {
		for candidate:=range candidateBlocks{
			mutex.Lock()
			tempBlocks = append(tempBlocks,candidate)
			mutex.Unlock()
		}
	}()

	//启动了一个Go runtine完成pickWinner函数
	go func() {
		for{
			pickWinner()
		}
	}()
	//接受验证着节点的链接

	for  {
		conn,err := server.Accept()
		if err != nil {
			log.Fatal(err)
		}
		go handleConn(conn)


	}

}

//生成区块
func generateBlock(oldBlock Block, BMP int, address string) (Block, error) {

	var newBlock Block
	t := time.Now()

	newBlock.Index = oldBlock.Index + 1
	newBlock.Timestamp = t.String()
	newBlock.BMP = BMP
	newBlock.PrevHash = oldBlock.Hash
	newBlock.Hash = calculateBlockHash(newBlock)
	newBlock.Validator = address
	return newBlock, nil

}

//hash
func calculateHash(s string) string {
	h := sha256.New()
	h.Write([]byte(s))
	hashed := h.Sum(nil)
	return hex.EncodeToString(hashed)

}

//计算区块hash值
func calculateBlockHash(block Block) string {

	record := string(block.Index) + block.Timestamp + string(block.BMP) + block.PrevHash
	return calculateHash(record)
}

//验证区块
func isBlockValid(newBlock, oldBlock Block) bool {
	if oldBlock.Index+1 != newBlock.Index {
		return false
	}

	if oldBlock.Hash != newBlock.PrevHash {
		return false
	}

	if calculateBlockHash(newBlock) != newBlock.Hash {
		return false
	}

	return true
}

func handleConn(conn net.Conn) {
	defer conn.Close()

	go func() {
		for {
			msg := <-announcements
			io.WriteString(conn, msg)

		}
	}()

	//验证者的地址
	var address string

	//验证着输入他所拥有的tokens，tokens的值越大，越容易获得区块的记账权

	io.WriteString(conn, "输入币的数量：")

	scanBalance := bufio.NewScanner(conn)
	for scanBalance.Scan() {
		//获取输入的数据，并将输入的值转换为int
		balance, err := strconv.Atoi(scanBalance.Text())

		if err != nil {
			log.Printf("%v 输入的不是一个数字", scanBalance.Text(), err)
			return
		}
		t := time.Now()
		//生成验证着的地址
		address = calculateHash(t.String())
		//将验证着的地址和token存储到validators
		validators[address] = balance
		fmt.Println(validators)
		break

	}

	io.WriteString(conn, "输入一个BMP：")
	scanBPM := bufio.NewScanner(conn)

	go func() {
		for {
			for scanBPM.Scan() {
				bmp, err := strconv.Atoi(scanBPM.Text())
				if err != nil {
					log.Printf("%v 不是一个数字：%v", scanBPM.Text(), err)
					delete(validators, address)
					conn.Close()
				}

				mutex.Lock()
				oldLastIndexBlock := Blockchain[len(Blockchain)-1]
				mutex.Unlock()

				//创建新的区块，然后将其发送到candidateBlocks通道
				newBlock, err := generateBlock(oldLastIndexBlock, bmp, address)
				if err != nil {
					log.Println(err)
					continue
				}
				if isBlockValid(newBlock, oldLastIndexBlock) {
					candidateBlocks <- newBlock
				}
				io.WriteString(conn, "\n输入一个新的BMP")

			}

		}
	}()

	for {
		time.Sleep(time.Minute)
		mutex.Lock()
		output, err := json.Marshal(Blockchain)
		mutex.Unlock()
		if err != nil {
			log.Fatal(err)
		}
		io.WriteString(conn, string(output)+"\n")

	}

}

//选举记账者
func pickWinner() {
	time.Sleep(30 * time.Second)
	mutex.Lock()
	temp := tempBlocks
	mutex.Unlock()

	lotterPool := []string{}

	if len(temp) > 0 {
	OUTER:
		for _, block := range temp {
			// 如果已经在lottery中存在，跳过
			for _, node := range lotterPool {
				if block.Validator == node {
					continue OUTER
				}
			}

			mutex.Lock()
			setValidators := validators
			mutex.Unlock()

			//获取验证者的token
			k, ok := setValidators[block.Validator]
			if ok {
				//向lottery中追加k条数据，k代表的是当前验证着的tokens
				for i := 0; i < k; i++ {
					lotterPool = append(lotterPool, block.Validator)
				}
			}

		}

		//通过随机获得获胜节点的地址
		s := rand.NewSource(time.Now().Unix())
		r := rand.New(s)
		lotteryWinner := lotterPool[r.Intn(len(lotterPool))]

		//把获胜者的区块添加到整条区块链上，然后通知所有的节点关于胜利者的消息
		for _, block := range temp {

			if block.Validator == lotteryWinner {
				mutex.Lock()
				Blockchain = append(Blockchain, block)
				mutex.Unlock()
				for _ = range validators {
					announcements <- "赢得了选举" + lotteryWinner

				}
				break
			}
		}

	}

	mutex.Lock()
	tempBlocks = []Block{}
	mutex.Unlock()

}








































