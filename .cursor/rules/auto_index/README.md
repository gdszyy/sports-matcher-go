# 自动函数索引 (Auto Index)

本目录由 `code-indexer` 脚本自动生成和维护，严禁手动编辑。

运行以下命令生成或更新索引：

```bash
# 全量生成
python3 /home/ubuntu/skills/code-indexer/scripts/generate_index.py <repo_path> --src-dirs src

# 单文件更新
python3 /home/ubuntu/skills/code-indexer/scripts/generate_index.py <repo_path> --file <relative_path>
```
