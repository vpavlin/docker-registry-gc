package main

import "fmt"
import "flag"
import "io/ioutil"
import "path"
import "encoding/json"
import "strings"
import "os"
import "os/exec"

import "github.com/termie/go-shutil"

var (
	registry_path = ""
	delete_path   = ""
	dry_run       = false
)

var used_images []string

type ancestry []string

type StringSet struct {
	set map[string]bool
}

type index_images []index_images_item

type index_images_item struct {
	Id string `json: "id"`
}

func main() {

	flag.Parse()

	fmt.Printf("%v\n", registry_path)
	fmt.Printf("%v\n", delete_path)

	initDeletePath()

	var image_list = getAllImageIds(registry_path)
	var image_names = getAllRepositories(registry_path)
	var used_images = getUsedImages(image_names)
	var all_used_images = NewSet()
	for _, i := range used_images {
		ids := getImageAncestry(i)
		for _, id := range ids {
			all_used_images.Add(id)
		}
	}

	var unused_images []string
	for _, i := range image_list {
		_, found := all_used_images.set[i]
		if !found {
			fmt.Printf("Unused image: %v\n", i)
			unused_images = append(unused_images, i)
			moveImage(i)
		}
	}

	for _, name := range image_names {
		_ = updateIndexImages(name, unused_images[0])
	}
	//	fmt.Println(all_images.Keys())
	fmt.Println(image_names)
	getUnusedSize()

}

func p(msg ...string) {
	if dry_run {
		fmt.Print("DRY RUN: ")
	}
	fmt.Println(strings.Join(msg, " "))
}

func init() {

	flag.StringVar(&registry_path, "registry-path", "/var/lib/docker-registry", "Path where your images and metadata are stored")
	flag.StringVar(&delete_path, "delete-path", "/var/lib/docker-registry-delete", "Path where deleted images and metadata will be stored")
	flag.BoolVar(&dry_run, "dry-run", false, "Don't perform any destructive changes on disk")

}

func initDeletePath() bool {

	deleted_image_path := path.Join(delete_path, "images")
	dp, err := os.Stat(deleted_image_path)
	if err != nil {
		err = os.MkdirAll(deleted_image_path, 0755)
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		} else {
			fmt.Println("Created", delete_path)
		}

	} else if dp != nil && !dp.IsDir() {
		fmt.Println("Path", delete_path, "exists and is not a directory")
		os.Exit(1)
	}

	return true
}

func NewSet() *StringSet {
	return &StringSet{make(map[string]bool)}
}

func (set *StringSet) Add(i string) bool {
	_, found := set.set[i]
	set.set[i] = true
	return !found //False if it existed already
}

func (set *StringSet) Keys() []string {
	keys := make([]string, len(set.set))
	for key := range set.set {
		keys = append(keys, key)
	}

	return keys
}

func getAllRepositories(registry_path string) (image_names []string) {
	repo_dir := path.Join(registry_path, "repositories")
	dirs, _ := ioutil.ReadDir(repo_dir)
	for _, repository := range dirs {
		images, _ := ioutil.ReadDir(path.Join(repo_dir, repository.Name()))
		for _, name := range images {
			image_names = append(image_names, path.Join(repository.Name(), name.Name()))
		}
	}

	return
}

func getUsedImages(image_names []string) (used_images []string) {
	for _, image := range image_names {
		tags, _ := ioutil.ReadDir(path.Join(registry_path, "repositories", image))
		for _, tag := range tags {
			if strings.HasPrefix(tag.Name(), "tag_") {
				data, _ := ioutil.ReadFile(path.Join(registry_path, "repositories", image, tag.Name()))
				used_images = append(used_images, string(data))
			}
		}
	}

	return
}

func getAllImageIds(registry_path string) (images []string) {
	//fmt.Println(registry_path)

	files, _ := ioutil.ReadDir(path.Join(registry_path, "images"))
	for _, f := range files {
		//       fmt.Printf("Visited: %s\n", f.Name())
		images = append(images, f.Name())
	}

	return
}

func getImageAncestry(image_path string) (ids []string) {
	data, _ := ioutil.ReadFile(path.Join(registry_path, "images", image_path, "ancestry"))
	_ = json.Unmarshal(data, &ids)

	return
}

func moveImage(image_path string) bool {
	src := path.Join(registry_path, "images", image_path)
	dst := path.Join(delete_path, "images", image_path)

	err := shutil.CopyTree(src, dst, nil)
	if err != nil {
		fmt.Println("Failed to move", image_path, ":", err)
		return false
	}
	if !dry_run {
		if err := os.Remove(src); err != nil {
			fmt.Println("Failed to remove", src, ":", err)
		}
	} else {
		p("Skipping removal of", src)
	}

	return true
}

func updateIndexImages(repository string, image string) bool {
	index_path := path.Join(registry_path, "repositories", repository, "_index_images")
	index_stat, _ := os.Stat(index_path)
	data, _ := ioutil.ReadFile(index_path)
	index := index_images{}
	_ = json.Unmarshal(data, &index)

	remove := -1
	for k, i := range index {
		if len(i.Id) > 0 {
			if i.Id == image {
				remove = k
				println(k, " ", i.Id)
				break
			}
		}
	}

	var new_index []byte
	if remove >= 0 {
		if remove < len(index)-1 {
			index = append(index[:remove], index[remove+1:]...)
		} else {
			index = index[:remove]
		}
		new_index, _ = json.Marshal(index)
		fmt.Println(string(new_index))

		index_bckp := path.Join(delete_path, "repositories", repository)
		err := os.MkdirAll(index_bckp, 0755)
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		} else {
			fmt.Println("Created", index_bckp)
		}

		err = shutil.CopyFile(index_path, path.Join(index_bckp, "_index_images"), false)
		if err != nil {
			fmt.Println("Couldn't backup _index_images for", repository, ":", err)
			return false
		}
		if !dry_run {
			err := ioutil.WriteFile(index_path, new_index, index_stat.Mode())
			if err != nil {
				fmt.Println("Failed to write _index_images for", repository, ":", err)
				return false
			}
		} else {
			p("Skipping write of new _index_images for", repository)
		}
	}

	return true
}

func getUnusedSize() {
	fmt.Println(delete_path)
	cmd := fmt.Sprintf("du -hc %v", delete_path)
	out, _ := exec.Command("sh", "-c", cmd).Output()

	out_arr := strings.Split(string(out), "\n")
	fmt.Println(string(out_arr[len(out_arr)-2]))
}
