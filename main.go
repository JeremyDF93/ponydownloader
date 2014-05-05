package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strconv"

	"github.com/NHOrus/ponydownloader/settings" //Here we are working with setting things up or down, depending.
)

//Default hardcoded variables
var (
	QDEPTH     int         = 20    //Depth of the queue buffer - how many images are enqueued
	IMGDIR                 = "img" //Default download directory
	TAG        string              //Default tag string is empty, it should be extracted from command line and only command line
	STARTPAGE  = 1                 //Default start page, derpiboo.ru 1-indexed
	STOPPAGE   = 0                 //Default stop page, would stop parsing json when stop page is reached or site reaches the end of search
	elog       *log.Logger         //The logger for errors
	KEY        string      = ""    //Default identification key. Get your own and place it in configuration, people
	SCRFILTER  int                 //So we can ignore things with limited
	FILTERFLAG = false             //Gah, not sure how to make it better.
)

type image struct {
	imgid    int
	url      string
	filename string
	score    int
	//	hash     string
}

func init() {

	Set := settings.Settings{QDepth: QDEPTH, ImgDir: IMGDIR, Key: KEY}

	Set.GetConfig(elog)

	QDEPTH = Set.QDepth
	KEY = Set.Key
	IMGDIR = Set.ImgDir

	//Here we are parsing all the flags. Command line argument hold priority to config. Except for 'key'. API-key is config-only
	flag.StringVar(&TAG, "t", TAG, "Tags to download")
	flag.IntVar(&STARTPAGE, "p", STARTPAGE, "Starting page for search")
	flag.IntVar(&STOPPAGE, "np", STOPPAGE, "Stopping page for search, 0 - parse all all search pages")
	flag.StringVar(&KEY, "k", KEY, "Your key to derpibooru API")
	flag.IntVar(&SCRFILTER, "scr", SCRFILTER, "Minimal score of image for it to be downloaded")
	flag.BoolVar(&FILTERFLAG, "filter", FILTERFLAG, "If set (to true), enables client-side filtration of downloaded images")

	flag.Parse()

}

func main() {

	fmt.Println("Derpiboo.ru Downloader version 0.2.0")

	elog, logfile := settings.SetLog() //Setting up logging of errors

	defer logfile.Close() //Almost forgot. Always close the file in the end.

	if flag.NArg() == 0 && TAG == "" { //If no arguments after flags and empty/unchanged tag, what we should download? Sane end of line.

		log.SetPrefix("Done at ")                //We can not do this with elog!
		log.Println("Nothing to download, bye!") //Need to reshuffle flow: now it could end before it starts.
		os.Exit(0)
	}

	//Creating directory for downloads if it does not yet exist
	if err := os.MkdirAll(IMGDIR, 0644); err != nil { //Execute? No need to execute any image. Also, all those other users can not do anything beyond enjoying our images.
		elog.Fatalln(err) //We can not create folder for images, end of line.
	}

	//	Creating channels to pass info to downloader and to signal job well done
	imgdat := make(chan image, QDEPTH) //Better leave default queue depth. Experiment shown that depth about 20 provides optimal perfomance on my system
	done := make(chan bool)

	if TAG == "" { //Because we can put imgid with flags. Why not?

		//	Checking argument for being a number and then getting image data

		imgid := flag.Arg(0) //0-indexed, unlike os.Args. os.Args[0] is path to program. It needs to be used later, when we are searching for what directory we are writing in
		_, err := strconv.Atoi(imgid)

		if err != nil {
			elog.Fatalln("Wrong input: can not parse", imgid, "as a number")
		}

		log.Println("Processing image No", imgid)

		go ParseImg(imgdat, imgid, KEY) // Sending imgid to parser. Here validity is our problem

	} else {

		//	and here we send tags to getter/parser. Validity is server problem, mostly

		log.Println("Processing tags", TAG)
		go ParseTag(imgdat, TAG, KEY)
	}

	log.Println("Starting worker") //It would be funny if worker goroutine does not start

	filtimgdat := make(chan image, QDEPTH)
	go FilterChannel(imgdat, filtimgdat) //see to move it into filter.Filter(inchan, outchan) where all filtration is done
	go DlImg(filtimgdat, done)

	<-done
	log.SetPrefix("Done at ")
	log.Println("Finised")
	//And we are done here! Hooray!
	return
}

func ParseImg(imgchan chan<- image, imgid string, KEY string) {

	source := "http://derpiboo.ru/images/" + imgid + ".json?nofav=&nocomments="
	if KEY != "" {
		source = source + "&key=" + KEY
	}

	fmt.Println("Getting image info at:", source)

	resp, err := http.Get(source) //Getting our nice http response. Needs checking for 404 and other responses that are... less expected
	if err != nil {
		elog.Println(err)
		return
	}

	defer resp.Body.Close() //and not forgetting to close it when it's done

	var dat map[string]interface{}

	body, err := ioutil.ReadAll(resp.Body) //stolen from official documentation
	if err != nil {
		elog.Println(err)
		return
	}

	if err := json.Unmarshal(body, &dat); //transforming json into native map

	err != nil {
		elog.Println(err)
		return
	}

	InfoToChannel(dat, imgchan)

	close(imgchan) //closing channel, we are done here

	return
}

func DlImg(imgchan <-chan image, done chan bool) {

	fmt.Println("Worker started; reading channel") //nice notification that we are not forgotten

	for {

		imgdata, more := <-imgchan

		if !more { //checking that there is an image in channel
			done <- true //well, there is no images in channel, it means we got them all, so synchronization is kicking in and ending the process
			break        //Just in case, so it would not stupidly die when program finishes - it will die smartly
		}

		if imgdata.filename == "" {
			elog.Println("Empty filename. Oops?") //something somewhere had gone wrong, not a cause to die, going to the next image
		} else {

			fmt.Println("Saving as", imgdata.filename)

			func() { // To not hold all the files open when there is no need. All pointers to files are in the scope of this function.

				output, err := os.Create(IMGDIR + string(os.PathSeparator) + imgdata.filename) //And now, THE FILE!
				if err != err {
					elog.Println("Error when creating file for image" + strconv.Itoa(imgdata.imgid))
					elog.Println(err) //Either we got no permisson or no space, end of line
					return
				}
				defer output.Close() //Not forgetting to deal with it after completing download

				response, err := http.Get(imgdata.url)
				if err != nil {
					elog.Println("Error when getting image", imgdata.imgid)
					elog.Println(err)
					return
				}
				defer response.Body.Close() //Same, we shall not listen to the void when we finished getting image

				io.Copy(output, response.Body) //	Writing things we got from Derpibooru into the file and into hasher

			}()
		}

		//fmt.Println("\n", hex.EncodeToString(hash.Sum(nil)), "\n", imgdata.hash )

	}
}

func ParseTag(imgchan chan<- image, tag string, KEY string) {

	source := "http://derpiboo.ru/search.json?nofav=&nocomments=" //yay hardwiring url strings!

	if KEY != "" {
		source = source + "&key=" + KEY
	}

	fmt.Println("Searching as", source+"&q="+tag)
	var working = true
	i := STARTPAGE
	for working {
		func() { //I suspect that all those returns could be dealt with in some way. But lazy.
			fmt.Println("Searching page", i)
			resp, err := http.Get(source + "&q=" + tag + "&page=" + strconv.Itoa(i)) //Getting our nice http response. Needs checking for 404 and other responses that are... less expected
			defer resp.Body.Close()                                                  //and not forgetting to close it when it's done. And before we panic and die horribly.
			if err != nil {
				elog.Println("Error while getting search page", i)
				elog.Println(err)
				return
			}

			var dats []map[string]interface{} //Because we got array incoming instead of single object, we using an slive of maps!

			//fmt.Println(resp)

			body, err := ioutil.ReadAll(resp.Body) //stolen from official documentation
			if err != nil {
				elog.Println("Error while reading search page", i)
				elog.Println(err)
				return
			}

			//fmt.Println(body)

			err = json.Unmarshal(body, &dats) //transforming json into native slice of maps

			if err != nil {
				elog.Println("Error while parsing search page", i)
				elog.Println(err)
				return

			}

			if len(dats) == 0 {
				fmt.Println("Pages are over") //Does not mean that process is over.
				working = false
				return
			} //exit due to finishing all pages

			for _, dat := range dats {
				InfoToChannel(dat, imgchan)
			}
			if i == STOPPAGE {
				working = false
				return
			}
			i++

		}()
	}

	close(imgchan)
}

func InfoToChannel(dat map[string]interface{}, imgchan chan<- image) {

	var imgdata image

	imgdata.url = "http:" + dat["image"].(string)
	//	imgdata.hash = dat["sha512_hash"].(string)
	imgdata.filename = (strconv.FormatFloat(dat["id_number"].(float64), 'f', -1, 64) + "." + dat["file_name"].(string) + "." + dat["original_format"].(string))
	imgdata.imgid = int(dat["id_number"].(float64))
	imgdata.score = int(dat["score"].(float64))

	//	for troubleshooting - possibly debug flag?
	//	fmt.Println(dat)
	//	fmt.Println(imgdata.url)
	//	fmt.Println(imgdata.hash)
	//	fmt.Println(imgdata.filename)

	imgchan <- imgdata
}

func FilterChannel(inchan <-chan image, outchan chan<- image) {

	for {

		imgdata, more := <-inchan

		if !more {
			close(outchan)
			return //Why make a bunch of layers of ifs if one can just end it all?
		}

		if !FILTERFLAG || (FILTERFLAG && imgdata.score >= SCRFILTER) {
			outchan <- imgdata
		} else {
			fmt.Println("Filtering " + imgdata.filename)
		}
	}
}
