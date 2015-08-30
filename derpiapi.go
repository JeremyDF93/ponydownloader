package main

import (
	"encoding/json"
	//	"fmt"
	"crypto/sha512"
	"encoding/hex"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

//Image contains data we got from API that we are using to filter and fetch said image next
type Image struct {
	Imgid          int    `json:"id_number"`
	URL            string `json:"image"`
	Filename       string
	Score          int    `json:"score"`
	OriginalFormat string `json:"original_format"`
	Hashval        string `json:"sha512_hash"`
}

//Search returns to us array of searched images...
type Search struct {
	Images []Image `json:"search"`
}

//ImageCh is a channel of image data. You can put images into channel by parsing
//Derpibooru API by id(s) or  by tags and you can download images that are already
//in channel
type ImageCh chan Image

//Push gets unmarchalled JSON info, massages it and plugs it into channel so it
//would be processed in other places
func (imgchan ImageCh) push(dat Image) {
	dat.Filename = strconv.Itoa(dat.Imgid) + "." + dat.OriginalFormat
	dat.URL = "https:" + dat.URL
	if dat.OriginalFormat == "svg" {
		i := strings.LastIndex(dat.URL, ".")
		if i != -1 {
			dat.URL = dat.URL[:i] + ".svg" //Was afraid to extract things I needed from the date field, so extracting them from URL.
		}
	}
	imgchan <- dat
}

//ParseImg gets image IDs, fetches information about those images from Derpibooru and pushes them into the channel.
func (imgchan ImageCh) ParseImg() {

	for _, imgid := range opts.Args.IDs {
		source := "https://derpiboo.ru/images/" + strconv.Itoa(imgid) + ".json"
		if opts.Key != "" {
			source = source + "?key=" + opts.Key
		}

		log.Println("Getting image info at:", source)

		response, err := http.Get(source) //Getting our nice http response. Needs checking for 404 and other responses that are... less expected
		if err != nil {
			elog.Println(err)
			continue
		}

		defer func() {
			err = response.Body.Close() //and not forgetting to close it when it's done
			if err != nil {
				elog.Fatalln("Could  not close server response")
			}
		}()
		var dat Image

		body, err := ioutil.ReadAll(response.Body) //stolen from official documentation
		if err != nil {
			elog.Println(err)
			continue
		}

		if err := json.Unmarshal(body, &dat); //transforming json into native map

		err != nil {
			elog.Println(err)
			return
		}

		imgchan.push(dat)
	}

	close(imgchan) //closing channel, we are done here

	return
}

//DlImg reads image data from channel and downloads specified images to disc
func (imgchan ImageCh) DlImg() {

	log.Println("Worker started; reading channel") //nice notification that we are not forgotten

	hasher := sha512.New()

	for {

		imgdata, more := <-imgchan

		if !more { //checking that there is an image in channel
			done <- true //well, there is no images in channel, it means we got them all, so synchronization is kicking in and ending the process
			break        //Just in case, so it would not stupidly die when program finishes - it will die smartly
		}

		if imgdata.Filename == "" {
			elog.Println("Empty filename. Oops?") //something somewhere had gone wrong, not a cause to die, going to the next image
			continue
		}

		log.Println("Saving as", imgdata.Filename)

		func() { // To not hold all the files open when there is no need. All pointers to files are in the scope of this function.

			output, err := os.Create(opts.ImageDir + string(os.PathSeparator) + imgdata.Filename) //And now, THE FILE!
			if err != err {
				elog.Println("Error when creating file for image" + strconv.Itoa(imgdata.Imgid))
				elog.Println(err) //Either we got no permisson or no space, end of line
				return
			}
			defer func() {
				err = output.Close() //Not forgetting to deal with it after completing download
				if err != nil {
					elog.Fatalln("Could  not close downloaded file")
				}
			}()
			start := time.Now()

			response, err := http.Get(imgdata.URL)
			if err != nil {
				elog.Println("Error when getting image", imgdata.Imgid)
				elog.Println(err)
				return
			}
			defer func() {
				err = response.Body.Close() //Same, we shall not listen to the void when we finished getting image
				if err != nil {
					elog.Fatalln("Could  not close server response")
				}
			}()

			size, err := io.Copy(output, io.TeeReader(response.Body, hasher)) //	Writing things we got from Derpibooru into the file and into hasher
			if err != nil {
				elog.Println("Unable to write image on disk, id ", imgdata.Imgid)
				elog.Println(err)
				return
			}
			timed := time.Since(start).Seconds()

			hash := hex.EncodeToString(hasher.Sum(nil))

			if hash != imgdata.Hashval {
				elog.Println("Hash mismatch, got ", hash, " instead of ", imgdata.Hashval)
			}

			hasher.Reset()

			log.Printf("Downloaded %d bytes in %.2fs, speed %s/s\n", size, timed, fmtbytes(float64(size)/timed))
		}()

		//fmt.Println("\n", hex.EncodeToString(hash.Sum(nil)), "\n", imgdata.hash )

	}
}

//ParseTag gets image tags, fetches information about all images it could from Derpibooru and pushes them into the channel.
func (imgchan ImageCh) ParseTag() {

	source := "https://derpiboo.ru/search.json?q=" + opts.Tag //yay hardwiring url strings!

	if opts.Key != "" {
		source = source + "&key=" + opts.Key
	}

	log.Println("Searching as", source)

	for i := opts.StartPage; opts.StopPage == 0 || i <= opts.StopPage; i++ {
		//I suspect that all those returns could be dealt with in some way. But lazy.
		log.Println("Searching page", i)

		response, err := http.Get(source + "&page=" + strconv.Itoa(i))
		//Getting our nice http response. Needs checking for 404 and other responses that are... less expected

		defer func() {
			err = response.Body.Close() //and not forgetting to close it when it's done. And before we panic and die horribly.
			if err != nil {
				elog.Fatalln("Could  not close server response")
			}
		}()

		if err != nil {
			elog.Println("Error while getting search page", i)
			elog.Println(err)
			continue
		}

		var dats Search //Because we got array incoming instead of single object, we using an slive of maps!

		//fmt.Println(resp)

		body, err := ioutil.ReadAll(response.Body) //stolen from official documentation
		if err != nil {
			elog.Println("Error while reading search page", i)
			elog.Println(err)
			continue
		}

		//fmt.Println(body)

		err = json.Unmarshal(body, &dats) //transforming json into native slice of maps

		if err != nil {
			elog.Println("Error while parsing search page", i)
			elog.Println(err)
			continue

		}

		if len(dats.Images) == 0 {
			log.Println("Pages are over") //Does not mean that process is over.
			break
		} //exit due to finishing all pages

		for _, dat := range dats.Images {
			imgchan.push(dat)
		}

	}

	close(imgchan)
}
