import json
from pathlib import Path
import tempfile
import unittest
from types import SimpleNamespace
from .d2_http_cache import CachePaths

class CachePathEvidence(unittest.TestCase):
    def test_exact_paths_and_all_values(self):
        with tempfile.TemporaryDirectory() as temp:
            rule={'id':'r1','url':'https://x.test/a.yaml','behavior':'domain','format':'yaml'}
            cache=CachePaths(SimpleNamespace(get_shared=lambda:{'rule_library':[rule]}),temp)
            reference=json.loads(Path(__file__).with_name('n89_cache_expected.json').read_text())['namespace'][4]
            old,native=reference['original'],reference['native']
            self.assertEqual(cache.aliases,{old:native})
            files={'shared/rules/'+old:'old bytes','shared/rules/unrecognized.list':'extra','other/'+old:'other'}
            projected=cache.files(files)
            self.assertEqual(projected['shared/rules/'+native],'old bytes')
            self.assertEqual(projected['shared/rules/unrecognized.list'],'extra')
            self.assertEqual(projected['other/'+old],'other')
            files['shared/rules/'+old]='changed bytes'
            self.assertEqual(cache.files(files)['shared/rules/'+native],'changed bytes')
            with self.assertRaises(AssertionError):
                cache.files({'shared/rules/'+old:'a','shared/rules/'+native:'b'})
            body={'results':[{'local_path':old,'success':False,'error':old,'other':'retained'}]}
            self.assertEqual(cache.cache_reply(body),{'results':[{'local_path':native,'success':False,'error':old,'other':'retained'}]})
            self.assertEqual(cache.path('by-id/'+Path(old).name),'by-id/'+Path(old).name)

    def test_seed_translation_preserves_time_and_unknown_files(self):
        with tempfile.TemporaryDirectory() as temp:
            rule={'id':'r1','url':'https://x.test/a.yaml','behavior':'domain','format':'yaml'}
            reference=json.loads(Path(__file__).with_name('n89_cache_expected.json').read_text())['namespace'][4]
            root=Path(temp)/'shared/rules'
            path=root/reference['native'];path.parent.mkdir(parents=True)
            path.write_text('valid seed')
            import os
            os.utime(path,ns=(1700000000123456789,1700000000123456789))
            unknown=root/'unknown.list';unknown.write_text('unknown')
            CachePaths(SimpleNamespace(get_shared=lambda:{'rule_library':[rule]}),temp)
            migrated=root/reference['original']
            self.assertEqual(migrated.read_text(),'valid seed')
            self.assertEqual(migrated.stat().st_mtime_ns,1700000000123456789)
            self.assertTrue(unknown.exists())

if __name__=='__main__': unittest.main()
