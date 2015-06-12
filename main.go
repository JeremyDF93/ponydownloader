package main

import (
	"fmt"
	"log"
	"os"
	"strconv"

	"github.com/NHOrus/ponydownloader/derpiapi" //Things we do with images and stuff
	flag "github.com/jessevdk/go-flags"
)

//Default hardcoded variables
var (
	elog *log.Logger //The logger for errors
)

func main() {
	fmt.Println("Derpiboo.ru Downloader version 0.4.0")

	err := flag.IniParse("config.ini", &opts)
	if err != nil {
		switch err.(type) {
			default:
				panic(err)
			case *os.PathError:
				fmt.Println("config.ini not found, using defaults")
		}
	}

	args, err := flag.Parse(&opts)
	if err != nil {
		flagError := err.(*flag.Error)
		if flagError.Type == flag.ErrHelp {
			return
		}
		if flagError.Type == flag.ErrUnknownFlag {
			fmt.Println("Use --help to view all available options")
			return
		}

		fmt.Printf("Error parsing flags: %s\n", err)
		return
	}

	logfile := SetLog(elog) //Setting up logging of errors

	defer logfile.Close() //Almost forgot. Always close the file in the end.
	
	WriteConfig(elog)	
	
	if len(args) == 0 && opts.Tag == "" { //If no arguments after flags and empty/unchanged tag, what we should download? Sane end of line.

		log.SetPrefix("Done at ")                //We can not do this with elog!
		log.Println("Nothing to download, bye!") //Need to reshuffle flow: now it could end before it starts.
		os.Exit(0)
	}

	//Creating directory for downloads if it does not yet exist
	err = os.MkdirAll(opts.ImageDir, 0755)

	if err != nil { //Execute bit means different thing for directories that for files. And I was stupid.
		elog.Fatalln(err) //We can not create folder for images, end of line.
	}

	//	Creating channels to pass info to downloader and to signal job well done
	imgdat := make(derpiapi.ImageCh, opts.QDepth) //Better leave default queue depth. Experiment shown that depth about 20 provides optimal perfomance on my system
	done := make(chan bool)

	if opts.Tag == "" { //Because we can put imgid with flags. Why not?

		//	Checking argument for being a number and then getting image data

		imgid := args[0]
		_, err := strconv.Atoi(imgid)

		if err != nil {
			elog.Fatalln("Wrong input: can not parse ", imgid, "as a number")
		}

		log.Println("Processing image No", imgid)

		go imgdat.ParseImg(imgid, opts.Key, elog) // Sending imgid to parser. Here validity is our problem

	} else {

		//	and here we send tags to getter/parser. Validity is server problem, mostly

		log.Println("Processing tags", opts.Tag)
		go imgdat.ParseTag(opts.Tag, opts.Key, opts.StartPage, opts.StopPage, elog)
	}

	log.Println("Starting worker") //It would be funny if worker goroutine does not start

	filtimgdat := make(derpiapi.ImageCh)
	fflag := derpiapi.FilterSet{Scrfilter: opts.Score, Filterflag: opts.Filter}

	go derpiapi.FilterChannel(imgdat, filtimgdat, fflag) //see to move it into filter.Filter(inchan, outchan) where all filtration is done

	go filtimgdat.DlImg(done, elog, opts.ImageDir)

	<-done
	log.SetPrefix("Done at ")
	log.Println("Finished")
	//And we are done here! Hooray!
	return
}
