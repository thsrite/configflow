package substore

// omap is a Python dict for the client's bookkeeping: insertion order,
// assigning an existing key keeps its position.
type omap[K comparable, V any] struct {
	order  []K
	values map[K]V
}

func newOmap[K comparable, V any]() *omap[K, V] { return &omap[K, V]{values: map[K]V{}} }

func (m *omap[K, V]) len() int { return len(m.order) }

func (m *omap[K, V]) has(key K) bool {
	_, ok := m.values[key]
	return ok
}

func (m *omap[K, V]) get(key K) (V, bool) {
	value, ok := m.values[key]
	return value, ok
}

func (m *omap[K, V]) set(key K, value V) {
	if _, ok := m.values[key]; !ok {
		m.order = append(m.order, key)
	}
	m.values[key] = value
}

func (m *omap[K, V]) delete(key K) {
	if _, ok := m.values[key]; !ok {
		return
	}
	delete(m.values, key)
	for i, k := range m.order {
		if k == key {
			m.order = append(m.order[:i:i], m.order[i+1:]...)
			break
		}
	}
}

// moveToEnd is OrderedDict.move_to_end.
func (m *omap[K, V]) moveToEnd(key K) {
	value, ok := m.values[key]
	if !ok {
		return
	}
	m.delete(key)
	m.set(key, value)
}

// keys is a snapshot of the keys in order.
func (m *omap[K, V]) keys() []K { return append([]K(nil), m.order...) }
