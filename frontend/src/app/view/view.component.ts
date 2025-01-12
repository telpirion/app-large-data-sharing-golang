import { Component, ElementRef, ViewChild ,inject} from '@angular/core';
import { Observable, of, Subject, from , take, fromEventPattern} from 'rxjs';
import { HttpClient } from '@angular/common/http';
import { Validators, FormBuilder, FormControl} from '@angular/forms';
import { ActivatedRoute, Router } from "@angular/router";
import { SessionStorageService } from '../service/session-storage.service';
import { FileModel } from '../type/file-model';
import { MainService } from '../service/main.service';
import { Subscription } from 'rxjs';
import * as ExifReader from 'exifreader';
import { DOCUMENT } from '@angular/common';

@Component({
  selector: 'app-view',
  templateUrl: './view.component.html',
  styleUrls: ['./view.component.scss']
})

export class ViewComponent {
  isOpen:boolean = false
  showUpdate: boolean = false;
  updateItem:any;
  deleteId: string = '';
  private fileSubject = new Subject<FileModel[]>();
  list$: Observable<null | any> = of([]);
  // listArr: FileModel[] = [];
  fileForm = this.fb.group({
    files: ['', Validators.required],
    tags: ['', Validators.required]
  })
  showLoader:boolean = true;
  showConfirmDialog:boolean = false;
  viewData:any;
  imgId:any
  onUploadFile: boolean = false;
  tags: string[] = [];

  document = inject (DOCUMENT);
  private tagsSubscription: Subscription = new Subscription;
  @ViewChild('viewImage') viewImage: ElementRef | undefined;

  constructor(
    private http: HttpClient,
    private fb: FormBuilder,
    private mainService: MainService,
    private router: Router,
    private activatedRoute: ActivatedRoute,
    private session: SessionStorageService
  ) { }

  ngOnInit(): void {
    this.list$ = this.fileSubject.asObservable();
    this.tagsSubscription = this.mainService.tagsSubject.subscribe(
      (tags: string[]) => {
        this.tags = tags;
      }
    );
    this.activatedRoute.params.pipe(take(1)).subscribe((paramMap: any) => {
      this.imgId = paramMap.imgId;
    });
    this.viewData = this.session.getImageData();
    const sizeInBytes = this.viewData.size;
    const sizeInKb = sizeInBytes / 1024;
    const sizeInMb = sizeInBytes / 1024 / 1024;

    const size = sizeInMb >= 1 ? `${Math.round((sizeInMb + Number.EPSILON) * 100) / 100} MB`
                              : `${Math.round((sizeInKb + Number.EPSILON) * 100) / 100} KB`;

    this.viewData.size = size;
    if(this.viewData && this.viewData.url) {
      this.viewData = this.viewData || {};

      ExifReader.load(`${this.document.location.origin}${this.viewData.url}`)
        .then((tags: any) => {
          const width = tags['Image Width']?.value || 'unknown';
          const height = tags['Image Height']?.value || 'unknown';
          this.viewData.resolution = `${width}x${height}`;
        })
        .catch((error: any) => {
          console.error('Error loading image Exif data:', error);
        });
    } else {
      console.error('Error: Invalid image URL');
    }

  }

  ngAfterViewInit(){
    this.showLoader = false;
  }

  toggleUploadFile(uploadSucess?: boolean): any{
    this.onUploadFile = !this.onUploadFile;
    if (uploadSucess){
      /* Need to clear ${this.tags} and redirect to home page
      */
        this.router.navigate(['/']);
    }
  }

  checkFileType(fileName: string): string {
   return this.mainService.checkFileType(fileName);
  }


  selectUpdate(item: string) {
    this.updateItem = item;
    this.showUpdate = true;
  }
  searchTags() {
    this.router.navigate(['list/',this.tags.join(' ')]);
  }

  tagsSearch(tag: any){
    this.router.navigate(['list/',tag]);
  }

  showConfirm(id: string){
    this.deleteId = id;
    this.showConfirmDialog = true;
  }
  cancel(){
    this.showConfirmDialog = false;
    this.deleteId = '';
  }
  delete() {
    const id = this.deleteId;
    if (!!id) {
      this.http.delete(`/api/files/${id}`).subscribe(res => {
        this.showConfirmDialog = false;
        this.deleteId = '';
        // const newFileArr = this.listArr;
        // const index = newFileArr.findIndex((file) => file.id === id);
        // if (index >= 0) {
          // newFileArr.splice(index,1);
          // this.fileSubject.next([...newFileArr]);
        // }
        this.router.navigate(['list/']);
      },
      err => {
        if (err.status === 404) {
          alert('The file you are trying to upload/update does not exist. Please update/upload a correct file.');
        }
      }
      )
    }

  }

  closePopup() {
    this.showUpdate = false;
  }


  handleSearchEvent(event: any) {
    if (event.eventName === "searchTags") {
      this.searchTags();
    } else if (event.eventName === "clearTag") {
      this.mainService.clearTag();
    }
  }

  updateImg(updateFile: any) {
    this.session.clearImageData();
    this.session.saveImageData(updateFile.file);
    this.viewData = updateFile.file;
    this.closePopup();
  }

}
