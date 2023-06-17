export function chunkArray<T>(array: T[], chunkSize: number): T[][] {
  let index = 0;
  let arrayLength = array.length;
  let tempArray = [];

  for (index = 0; index < arrayLength; index += chunkSize) {
    let myChunk = array.slice(index, index + chunkSize);
    tempArray.push(myChunk);
  }

  return tempArray;
}
