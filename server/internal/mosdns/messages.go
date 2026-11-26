package mosdns

const msgCustomYAML = "MosDNS 自定义 YAML 无效：%v"

// msgCustomConfigNotText: a stored custom_config that is not text (read as
// model.DamagedText) is refused, never generated as an empty base.
const msgCustomConfigNotText = "mosdns.custom_config 不是文本，请在配置页重新填写后保存"

// PayloadCollectionMessage identifies a scalar where a YAML payload collection is required.
const PayloadCollectionMessage = "MosDNS YAML payload 必须是列表或对象"

const msgProxyTokenRequired = "MosDNS rule proxy requires a rule proxy token"
