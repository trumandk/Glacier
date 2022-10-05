# Glacier Archive Server 
Glacier is essentially a storage database with high write performance and cohesive delete performance.  
  
It's design is simple (~300 line of code) and data is simply saved into Tar archives identified by Time-UUID. The Time-UUID is a version-4 UUID containing a human readable timestamp(`YYYYMMDD-HHMM`) in the first two sections. The timestamp identifies the exact location on disk by mapping the timestamp into a folder-age-tree (`/YYYY/MM/DD/HH/`). Each subfolder (for each hours) contains 256 Tar archives where the last two UUID hex-values identify the archive it is associated. Splitting each hour into 256 Tar archives increases write performance by limiting write-lock to the same archive, and thereby 256 thread can write/read simultaniously in each hour section. 

`(A blob with Time-UUID "20211218-1036-40f1-b34f-02d7517a01d3" will be appended into "/2021/12/18/10/d3.tar")`


Pros
- Simple design and API
- High write performance
- High delete performance, for aging-out(deletion) of old data
- Automatic age-off oldest data when storage limit is reached
- Optimized for all filesizes (1 byte to 8GB)
- Unlimited numbers of blobs
- The number of files on disk is always known(maximum 256 Tar archives per hour)
- Build in prometheus support.
- Build in ACME support.

Cons
- Not possible to delete or overwrite single blob, it must age-off.
- Only accept Time-UUID as identifiers.

## Ongoing work
- S3 support
- Blob encryption
- Blob metadata
