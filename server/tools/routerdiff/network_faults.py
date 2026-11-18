"""Reversible SYN drops restricted to the worker's private loopback namespace."""
import json
import os
import re
import subprocess


class NetworkFaults:
    def __init__(self):
        self.enabled = False

    @staticmethod
    def guard():
        parent = os.environ.get('ROUTERDIFF_PARENT_NETNS', '')
        current = os.readlink('/proc/self/ns/net')
        if (os.environ.get('ROUTERDIFF_ISOLATED') != '1' or not re.fullmatch(r'net:\[[0-9]+\]', parent)
                or current == parent):
            raise RuntimeError('network faults require a distinct owned network namespace')
        links = json.loads(subprocess.run(['ip', '-j', 'link', 'show'], check=True,
            stdout=subprocess.PIPE, text=True, timeout=5).stdout)
        if len(links) != 1 or links[0].get('ifname') != 'lo':
            raise RuntimeError('network faults require a loopback-only namespace')

    @staticmethod
    def nft(*arguments, script=None):
        return subprocess.run(['sudo', 'nft', *arguments], input=script, check=True,
            stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True, timeout=5).stdout

    def saw_syn(self):
        state = json.loads(self.nft('-j', 'list', 'chain', 'ip', 'routerdiff_transport', 'output'))
        counters = [item['counter']['packets'] for entry in state['nftables']
                    for item in entry.get('rule', {}).get('expr', []) if 'counter' in item]
        if len(counters) != 1 or type(counters[0]) is not int:
            raise RuntimeError('owned SYN-drop rule has no unique packet counter')
        return counters[0] > 0

    def control(self, enabled):
        self.guard()
        if type(enabled) is not bool or enabled == self.enabled:
            raise ValueError('network fault must change its explicit boolean state')
        if enabled:
            # All addresses, names and ports are constants. No caller-supplied
            # command, host, interface or nft syntax can enter this operation.
            self.nft('-f', '-', script='''add table ip routerdiff_transport
add chain ip routerdiff_transport output { type filter hook output priority 0; policy accept; }
add rule ip routerdiff_transport output ip daddr 10.44.0.3 tcp dport 18082 tcp flags & (syn | ack) == syn counter drop
''')
            self.enabled = True
            return {'enabled': True, 'saw_syn': self.saw_syn()}
        observed = self.saw_syn()
        self.close()
        return {'enabled': False, 'saw_syn': observed}

    def close(self):
        if self.enabled:
            self.guard()
            self.nft('delete', 'table', 'ip', 'routerdiff_transport')
            self.enabled = False
