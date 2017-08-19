package controller

import (
	"fmt"
	"runtime"
	"time"

	"frank/src/go/devices"
	"frank/src/go/helpers"
	"frank/src/go/helpers/log"
	"frank/src/go/managers"
	"frank/src/go/services"
)

const developerKey = "AIzaSyBEsKHzV5PkHUhvEOKjYfefv7_tkZ8EREs"
const (
	Stopped = 0
	Paused  = 1
	Running = 2
)

const timeout = 60 * time.Second

type FrankController struct {
	VoiceRecognition services.VoiceRecognition
	Config           managers.Config

	timer     *time.Timer
	keywordCh chan int
	voiceCh   chan int
	killCh    chan bool
}

func NewFrankController() (FrankController, error) {
	helpers.LoadDirs()

	frankController := FrankController{}
	log.InitLogger()
	c, err := managers.GetConfig(helpers.ConfigDir)
	if err != nil {
		return frankController, err
	}

	frankController.Config = c

	voiceRecognition, err := services.NewVoiceRecognition(frankController.Config.Get("google_api_key"))
	frankController.VoiceRecognition = voiceRecognition

	return frankController, nil
}

func (fc *FrankController) Start() {
	log.Log.Info(fc.Config.Name)
	fc.keywordCh = make(chan int)
	fc.voiceCh = make(chan int)
	fc.killCh = make(chan bool, 1)

	log.Log.Info("Starting Keyword Recognition")
	go fc.StartKeywordRecognition()

	var input string
	fmt.Scanln(&input)
}

func (fc *FrankController) VoiceRecognitionToText(fileName string) {
	fmt.Printf("#goroutines: %d\n", runtime.NumGoroutine())
	log.Log.Debugf("[%s] Analyzing Audio", fileName)
	text := fc.VoiceRecognition.AnalyzeAudio(fileName)

	helpers.RemoveRecordFile(fileName)

	log.Log.Debugf("[%s] Found Text: %s", fileName, text)
	commands := services.CheckCommands(text, fc.Config.Commands)
	go devices.ManageCommands(commands, &fc.Config)
	fc.CheckDeactivation(fileName, text)
}

func (fc *FrankController) CheckDeactivation(fileName string, text string) {
	log.Log.Debugf("[%s] Checking Deactivation Keywords", fileName)

	for _, sentence := range fc.Config.Deactivation {
		if sentence == text {
			log.Log.Infof("[%s] Deactivation Keyword Found", fileName)
			fc.StopVoiceRecognition()
			return
		}
	}
	fc.timer.Reset(timeout)
}

func (fc *FrankController) StopVoiceRecognition() {
	log.Log.Debug("Stopping Voice Recognition And starting Keyword Recognition")
	fc.killCh <- true
	fc.voiceCh <- Stopped
	fc.keywordCh <- Running
}

func (fc *FrankController) StartVoiceRecognition() {
	state := Running
	log.Log.Debug("Started timeout to deactivate voice recognition")
	fc.timer = time.AfterFunc(timeout, fc.StopVoiceRecognition)
	for {
		select {
		case state = <-fc.voiceCh:
			switch state {
			case Stopped:
				log.Log.Info("Stopped Voice Recognition")
				fc.keywordCh <- Running
				return
			case Running:
				log.Log.Info("Started Voice Recognition")
			case Paused:
				log.Log.Info("Paused Voice Recognition")
			}
		default:
			if state == Paused {
				time.Sleep(1 * time.Second)
				break
			}
			log.Log.Info("Listening Voice")
			fileName, _ := services.StartRecord(fc.killCh)
			if fileName == "" {
				break
			}
			go fc.VoiceRecognitionToText(fileName)
		}
	}
}
func (fc *FrankController) StartKeywordRecognition() {
	state := Running
	for {
		select {
		case state = <-fc.keywordCh:
			switch state {
			case Stopped:
				log.Log.Info("Stopped Keyword Recognition")
				return
			case Running:
				log.Log.Info("Started Keyword Recognition")
			case Paused:
				log.Log.Info("Paused Keyword Recognition")
			}
		default:
			if state == Paused {
				time.Sleep(1 * time.Second)
				break
			}

			log.Log.Info("Listening Keyword")
			fileName, _ := services.StartRecord(fc.killCh)
			if fileName == "" {
				break
			}

			result := services.KeywordRecognition(fileName)
			helpers.RemoveRecordFile(fileName)
			if result == true {
				log.Log.Debug("Keyword matched")
				go fc.StartVoiceRecognition()
				state = Paused
				break
			} else {
				log.Log.Debug("Keyword not matched")
				go fc.StartVoiceRecognition()
				state = Paused
				break
			}
		}
		time.Sleep(30)
	}
}
