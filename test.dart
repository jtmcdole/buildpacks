import 'dart:io';

main() async {
  final foo = """
  echo "This is a multiline command"
  echo "with multiple lines"
  echo "and arguments with spaces"
""";

  print("will try to run $foo");

  final result =  await Process.run('sh', ['-c', foo]);
  print(result.stdout);
  print(result.stderr);

}
