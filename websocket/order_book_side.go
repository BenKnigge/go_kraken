package websocket

import (
	"bytes"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/ericlagergren/decimal"
)

type orderBookLevel struct {
	Price  *decimal.Big
	Volume *decimal.Big
}

type byPrice []orderBookLevel

func (a byPrice) Len() int           { return len(a) }
func (a byPrice) Less(i, j int) bool { return a[i].Price.Cmp(a[j].Price) == -1 }
func (a byPrice) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }

func newOrderBookLevels(m map[string]orderBookLevel, asc bool) []orderBookLevel {
	result := make([]orderBookLevel, 0)

	for _, value := range m {
		result = append(result, value)
	}

	if asc {
		sort.Sort(byPrice(result))
	} else {
		sort.Sort(sort.Reverse(byPrice(result)))
	}

	return result
}

// OrderBookSide -
type OrderBookSide struct {
	m               map[string]orderBookLevel
	sorted          []orderBookLevel
	depth           int
	pricePrecision  int
	volumePrecision int
	isAsk           bool

	mx *sync.RWMutex
}

func newOrderBookSide(depth, pricePrecision, volumePrecision int, isAsk bool) *OrderBookSide {
	return &OrderBookSide{
		m:               make(map[string]orderBookLevel),
		sorted:          make([]orderBookLevel, 0),
		depth:           depth,
		pricePrecision:  pricePrecision,
		volumePrecision: volumePrecision,
		isAsk:           isAsk,
		mx:              new(sync.RWMutex),
	}
}

func stringFixed(big *decimal.Big, precision int) string {
	fmtStr := "%." + strconv.Itoa(precision) + "f"
	return fmt.Sprintf(fmtStr, big)
}

func (o *OrderBookSide) applyUpdate(upd OrderBookItem) error {
	flValue, err := upd.Volume.Float64()
	if err != nil {
		return err
	}

	price := decimal.WithPrecision(o.pricePrecision)
	err = price.UnmarshalText([]byte(upd.Price.String()))
	if err != nil {
		return err
	}

	key := stringFixed(price, o.pricePrecision)

	o.mx.Lock()
	if flValue == 0 {
		delete(o.m, key)
	} else {
		v := &decimal.Big{}
		err = v.UnmarshalText([]byte(upd.Price.String()))
		if err != nil {
			return err
		}
		o.m[key] = orderBookLevel{
			Price:  price,
			Volume: v,
		}
	}
	o.mx.Unlock()
	return nil
}

func (o *OrderBookSide) applyUpdates(updates []OrderBookItem) error {
	for i := range updates {
		if err := o.applyUpdate(updates[i]); err != nil {
			return err
		}
	}

	o.mx.Lock()
	levels := newOrderBookLevels(o.m, o.isAsk)
	for _, level := range levels[o.depth:] {
		delete(o.m, stringFixed(level.Price, o.pricePrecision))
	}
	o.sorted = levels[:o.depth]
	o.mx.Unlock()

	return nil
}

// Get - receives volume by price. If not exists returns false
func (o *OrderBookSide) Get(price *decimal.Big) (*decimal.Big, bool) {
	o.mx.RLock()
	defer o.mx.RUnlock()

	key := stringFixed(price, o.pricePrecision)
	level, ok := o.m[key]
	if !ok {
		return decimal.New(0, 0), ok
	}
	return level.Volume, ok
}

// Range - ranges by order book side from best price to depth
func (o *OrderBookSide) Range(handler func(price, volume *decimal.Big) error) error {
	o.mx.RLock()
	defer o.mx.RUnlock()

	for i := range o.sorted {
		if err := handler(o.sorted[i].Price, o.sorted[i].Volume); err != nil {
			return err
		}
	}
	return nil
}

// Best - returns best price and volume at this price. If order book is not initialized it returns Zero
func (o *OrderBookSide) Best() (*decimal.Big, *decimal.Big) {
	o.mx.RLock()
	defer o.mx.RUnlock()

	if len(o.sorted) == 0 {
		return decimal.New(0, 0), decimal.New(0, 0)
	}
	return o.sorted[0].Price, o.sorted[0].Volume
}

func (o *OrderBookSide) checksum() []byte {
	o.mx.RLock()
	defer o.mx.RUnlock()

	var str bytes.Buffer
	for _, level := range o.sorted {
		price := stringFixed(level.Price, o.pricePrecision)
		price = strings.Replace(price, ".", "", 1)
		price = strings.TrimLeft(price, "0")
		str.WriteString(price)

		volume := stringFixed(level.Volume, o.volumePrecision)
		volume = strings.Replace(volume, ".", "", 1)
		volume = strings.TrimLeft(volume, "0")
		str.WriteString(volume)
	}
	return str.Bytes()
}

// String -
func (o *OrderBookSide) String() string {
	o.mx.RLock()
	defer o.mx.RUnlock()

	var str strings.Builder
	for i := range o.sorted {
		str.WriteByte('\t')
		if o.pricePrecision > 0 {
			str.WriteString(stringFixed(o.sorted[i].Price, o.pricePrecision))
		} else {
			str.WriteString(o.sorted[i].Price.String())
		}
		str.WriteString(" [ ")
		if o.volumePrecision > 0 {
			str.WriteString(stringFixed(o.sorted[i].Volume, o.volumePrecision))
		} else {
			str.WriteString(o.sorted[i].Volume.String())
		}
		str.WriteString(" ]\r\n")
	}
	return str.String()
}
