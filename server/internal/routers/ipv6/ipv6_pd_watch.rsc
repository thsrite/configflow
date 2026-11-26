:local lan "@@LAN@@"
:local pool "@@POOL@@"
:local hold 3600
:local tag "cf-pd: deprecated until "
:local now [:tonum [:timestamp]]
:local ids [/ipv6 address find where interface=$lan from-pool=$pool disabled=no invalid=no]
:if ([:len $ids] = 1) do={
  :local a [:tostr [/ipv6 address get ($ids->0) address]]
  :local cur ([:tostr ([:toip6 [:pick $a 0 [:find $a "/"]]] & ffff:ffff:ffff:ffff::)] . "/64")
  :if ([:pick $cur 0 2] != "::") do={
    # A prefix that is current again must not stay deprecated; the static
    # entry also blocks the dynamic one, so re-trigger advertising.
    :local revived false
    :foreach i in=[/ipv6 nd prefix find where comment~"^cf-pd: deprecated"] do={
      :if ([:tostr [/ipv6 nd prefix get $i prefix]] = $cur) do={ /ipv6 nd prefix remove $i; :set revived true }
    }
    :if ($revived) do={
      /ipv6 address set ($ids->0) advertise=no
      /ipv6 address set ($ids->0) advertise=yes
      :log warning ("cf-pd: " . $cur . " is current again, re-advertised it")
    }
    :local id [/ipv6 firewall address-list find where list="cf-pd-last"]
    :if ([:len $id] = 0) do={
      /ipv6 firewall address-list add list="cf-pd-last" address=$cur comment="cf-pd: current LAN prefix"
      :log info ("cf-pd: recorded " . $cur)
    } else={
      :local old [:tostr [/ipv6 firewall address-list get ($id->0) address]]
      :if ($old != $cur) do={
        :foreach i in=[/ipv6 nd prefix find where comment~"^cf-pd: deprecated"] do={
          :if ([:tostr [/ipv6 nd prefix get $i prefix]] = $old) do={ /ipv6 nd prefix remove $i }
        }
        /ipv6 nd prefix add interface=$lan prefix=$old preferred-lifetime=0s valid-lifetime=10m comment=($tag . ($now + $hold))
        /ipv6 firewall address-list set ($id->0) address=$cur
        :log warning ("cf-pd: LAN prefix changed " . $old . " -> " . $cur)
      }
    }
  }
}
:foreach i in=[/ipv6 nd prefix find where comment~"^cf-pd: deprecated until "] do={
  :local c [/ipv6 nd prefix get $i comment]
  :if ([:tonum [:pick $c [:len $tag] [:len $c]]] <= $now) do={
    :log info ("cf-pd: stopped advertising deprecated " . [:tostr [/ipv6 nd prefix get $i prefix]])
    /ipv6 nd prefix remove $i
  }
}
