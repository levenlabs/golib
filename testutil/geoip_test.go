package testutil

import (
	"net"
	. "testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGeoIP2Reader(t *T) {
	r, err := GeoIP2Reader()
	require.NoError(t, err)

	ip := net.ParseIP(GeoIP2IPv4)
	c, err := r.City(ip)
	require.NoError(t, err)
	assert.Equal(t, "Milton", c.City.Names["en"])
	assert.Equal(t, "United States", c.Country.Names["en"])
	assert.Equal(t, "North America", c.Continent.Names["en"])
	require.Len(t, c.Subdivisions, 1)
	assert.Equal(t, "Washington", c.Subdivisions[0].Names["en"])
	assert.Equal(t, "98354", c.Postal.Code)
	assert.Equal(t, "US", c.Country.IsoCode)
	assert.Equal(t, "NA", c.Continent.Code)

	ip = net.ParseIP(GeoIP2IPv6)
	c, err = r.City(ip)
	require.NoError(t, err)
	assert.Equal(t, "Finland", c.Country.Names["en"])
	assert.Equal(t, "Europe", c.Continent.Names["en"])
	assert.Equal(t, "FI", c.Country.IsoCode)
	assert.Equal(t, "EU", c.Continent.Code)
}
