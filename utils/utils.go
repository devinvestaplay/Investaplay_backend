package utils

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"slices"
	"time"
)

func PrintJson(v any) {
	jsonData, err := json.Marshal(v)
	if err != nil {
		fmt.Println("[MSVOIDEV]=> Error serializing state to JSON:", err)
	}
	fmt.Println(string(jsonData))
}

func SerializeObjectToString[T any](obj *T) (string, error) {
	jsonData, err := json.Marshal(obj)
	if err != nil {
		return "", err
	}
	return string(jsonData), nil
}

func SerializeObjectToByteArray[T any](obj *T) ([]byte, error) {
	jsonData, err := json.Marshal(obj)
	if err != nil {
		return nil, err
	}
	return jsonData, nil
}

func DeserializeObjectFromString[T any](jsonStr *string) (T, error) {
	var obj T
	err := json.Unmarshal([]byte(*jsonStr), &obj)
	if err != nil {
		return obj, err
	}
	return obj, nil
}

func DeserializeObjectFromStringByType[T any](jsonStr *string, obj *T) error {
	err := json.Unmarshal([]byte(*jsonStr), obj)
	if err != nil {
		return err
	}
	return nil
}

func DeserializeObjectFromStringByRefs[T any](jsonStr *string, obj *T) error {
	err := json.Unmarshal([]byte(*jsonStr), obj)
	if err != nil {
		return err
	}
	return nil
}

func DeserializeObjectFromByteArray[T any](jsonByteArray []byte) (T, error) {
	var obj T
	err := json.Unmarshal(jsonByteArray, &obj)
	if err != nil {
		return obj, err
	}
	return obj, nil
}

func DeserializeObjectFromByteArrayByRefs[T any](jsonByteArray *[]byte, obj *T) error {
	err := json.Unmarshal(*jsonByteArray, obj)
	if err != nil {
		return err
	}
	return nil
}

func DeserializeObjectFromStringByRefsToMap(jsonStr *string, payloadMap *map[string]interface{}) error {
	err := json.Unmarshal([]byte(*jsonStr), payloadMap)
	if err != nil {
		return err
	}
	return nil
}

//	func DeserializeObjectFromStringByRefsToMapGeneric[T any](jsonStr *string, payloadMap *map[string]T) error {
//		err := json.Unmarshal([]byte(*jsonStr), payloadMap)
//		if err != nil {
//			return err
//		}
//		return nil
//	}
func DeserializeObjectFromStringByRefsToMapGeneric[T any, K comparable](jsonStr *string, payloadMap *map[K]T) error {
	err := json.Unmarshal([]byte(*jsonStr), payloadMap)
	if err != nil {
		return err
	}
	return nil
}

func DeepCopyMap[K comparable, V any](src map[K]V) map[K]V {
	dst := make(map[K]V)
	for key, value := range src {
		dst[key] = value
	}
	return dst
}

func DeepCopySlice[T any](src []T) []T {
	dst := make([]T, len(src))
	copy(dst, src)
	return dst
}

func GenerateUniqueNumericUsername() string {
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	// generate 10 digits
	// 10 digits (1000 to 99,999,000)
	randomNumber := r.Int63n(99990000) + 1000

	return fmt.Sprintf("%d", randomNumber)
}

func ReorderList[T comparable](fourList []T, twoList []T) []T {
	reordered := make([]T, 0, len(fourList))

	var inList2 []T
	var notInList2 []T

	for _, data := range fourList {
		if slices.Contains(twoList, data) {
			inList2 = append(inList2, data)
		} else {
			notInList2 = append(notInList2, data)
		}
	}

	if len(inList2) == 2 && len(notInList2) == 2 {

		reordered = append(reordered, inList2[0])
		reordered = append(reordered, notInList2[0])
		reordered = append(reordered, inList2[1])
		reordered = append(reordered, notInList2[1])
	}

	return reordered
}
